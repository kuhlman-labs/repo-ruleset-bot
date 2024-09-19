package reporulesetbot

import (
	"context"
	"encoding/json"
	"os"

	"github.com/google/go-github/v63/github"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// Changes represents the changes in a ruleset event.
type Changes struct {
	Name struct {
		From string `json:"from,omitempty"`
	} `json:"name,omitempty"`
	Enforcement struct {
		From string `json:"from,omitempty"`
	} `json:"enforcement,omitempty"`
}

// Workflows represents the ruleset workflows parameters.
type Workflows struct {
	Workflows []Workflow `json:"workflows"`
}

// Workflow represents a workflow.
type Workflow struct {
	RepositoryID int64  `json:"repository_id"`
	Path         string `json:"path"`
	Ref          string `json:"ref"`
}

// readMultipleRulesets reads muliple rulesets from JSON files.
func (h *RulesetHandler) readMultipleRulesets(ctx context.Context, client *github.Client, orgName string, logger zerolog.Logger) ([]*github.Ruleset, error) {
	var rulesets []*github.Ruleset

	filenames, err := getRuleSetFiles()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get ruleset files")
	}

	for _, filename := range filenames {
		ruleset, err := h.readRulesetFromFile(filename, ctx, client, orgName, logger)
		if err != nil {
			return nil, err
		}
		rulesets = append(rulesets, ruleset)
	}
	return rulesets, nil
}

// readRulesetFromFile reads the ruleset from a JSON file.
func (h *RulesetHandler) readRulesetFromFile(filename string, ctx context.Context, client *github.Client, orgName string, logger zerolog.Logger) (*github.Ruleset, error) {
	logger.Info().Msgf("Processing ruleset file %s...", filename)

	jsonData, err := os.ReadFile(filename)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to read ruleset file %s.", filename)
		return nil, errors.Wrap(err, "Failed to read ruleset file")
	}

	var ruleset *github.Ruleset
	if err := json.Unmarshal(jsonData, &ruleset); err != nil {
		logger.Error().Err(err).Msgf("Failed to unmarshal ruleset file %s.", filename)
		return nil, errors.Wrap(err, "Failed to unmarshal ruleset file")
	}

	if err := h.processRuleset(ctx, ruleset, client, orgName, logger); err != nil {
		return nil, err
	}

	logger.Info().Msgf("Processed ruleset file %s.", filename)

	return ruleset, nil
}

// processRuleset processes the ruleset.
func (h *RulesetHandler) processRuleset(ctx context.Context, ruleset *github.Ruleset, client *github.Client, orgName string, logger zerolog.Logger) error {
	sourceOrgName := ruleset.Source

	for _, rule := range ruleset.Rules {
		if rule.Type == "workflows" {
			if err := processWorkflows(ctx, rule, client, orgName, logger); err != nil {
				return errors.Wrapf(err, "Failed to process workflows in ruleset file: %s", ruleset.Name)
			}
		}
	}

	for _, bypassActor := range ruleset.BypassActors {
		if shouldProcessBypassActor(bypassActor) {
			switch bypassActor.GetActorType() {
			case "Team":
				if err := h.processTeamActor(ctx, client, bypassActor, sourceOrgName, orgName); err != nil {
					return errors.Wrapf(err, "Failed to process team bypass actor with id %d in ruleset file: %s", bypassActor.GetActorID(), ruleset.Name)
				}
			case "RepositoryRole":
				if err := h.processRepoRoleActor(ctx, client, bypassActor, sourceOrgName, orgName); err != nil {
					return errors.Wrapf(err, "Failed to process repository role bypass actor with id %d in ruleset file: %s", bypassActor.GetActorID(), ruleset.Name)
				}
			case "Integration":
				continue
			default:
				logger.Warn().Msgf("Unhandled actor type: %s", bypassActor.GetActorType())
			}
		}
	}
	return nil
}

// shouldProcessBypassActor returns true if the bypass actor should be processed.
func shouldProcessBypassActor(bypassActor *github.BypassActor) bool {
	actorID := bypassActor.GetActorID()
	return actorID != 0 && actorID > 5
}

// processWorkflows processes the workflows in a repository rule.
func processWorkflows(ctx context.Context, rule *github.RepositoryRule, client *github.Client, orgName string, logger zerolog.Logger) error {
	var workflows Workflows
	if err := json.Unmarshal(*rule.Parameters, &workflows); err != nil {
		logger.Error().Err(err).Msg("Failed to unmarshal workflow parameters.")
		return errors.Wrap(err, "Failed to unmarshal workflow parameters")
	}

	for i, workflow := range workflows.Workflows {
		if err := updateWorkflowRepoID(ctx, &workflow, client, orgName, logger); err != nil {
			return err
		}
		workflows.Workflows[i] = workflow
	}

	updatedWorkflowsJSON, err := json.Marshal(workflows)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to marshal updated workflows.")
		return errors.Wrap(err, "Failed to marshal updated workflows")
	}

	*rule.Parameters = updatedWorkflowsJSON
	return nil
}

// updateWorkflowRepoID updates the repository ID in a workflow.
func updateWorkflowRepoID(ctx context.Context, workflow *Workflow, client *github.Client, orgName string, logger zerolog.Logger) error {
	repoName, err := getRepoName(ctx, client, workflow.RepositoryID)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get repository name")
		return errors.Wrapf(err, "Failed to get repository name for repository ID %d", workflow.RepositoryID)
	}

	newRepoID, err := getRepoID(ctx, client, orgName, repoName)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get repository ID.")
		return errors.Wrapf(err, "Failed to get repository ID for repository %s/%s", orgName, repoName)
	}

	workflow.RepositoryID = newRepoID
	return nil
}

// processTeamActor processes a team actor.
func (h *RulesetHandler) processTeamActor(ctx context.Context, client *github.Client, actor *github.BypassActor, sourceOrgName, orgName string) error {

	// create jwt client
	jwtclient, err := newJWTClient()
	if err != nil {
		return errors.Wrap(err, "Failed to create JWT client")
	}

	// get installation for the app
	installation, err := getOrgInstallationID(ctx, jwtclient, sourceOrgName)
	if err != nil {
		return errors.Wrap(err, "Failed to get installation for the app")
	}

	// create installation client
	sourceClient, err := h.ClientCreator.NewInstallationClient(installation)
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	// get org ID
	orgID, err := getOrgID(ctx, sourceClient, sourceOrgName)
	if err != nil {
		return errors.Wrap(err, "Failed to get org ID")
	}

	teamID := actor.GetActorID()

	sourceTeam, err := getTeamByID(ctx, sourceClient, orgID, teamID)
	if err != nil {
		errors.Wrapf(err, "Failed to get team with ID %d", teamID)
		return err
	}

	teamName := sourceTeam.GetSlug()

	newTeam, err := getTeamByName(ctx, client, orgName, teamName)
	if err != nil {
		errors.Wrapf(err, "Failed to get team with name %s", teamName)
		return err
	}

	teamID = newTeam.GetID()

	actor.ActorID = &teamID

	return nil
}

// processRepoRoleActor processes a repository role actor.
func (h *RulesetHandler) processRepoRoleActor(ctx context.Context, client *github.Client, actor *github.BypassActor, sourceOrgName, orgName string) error {
	actorID := actor.GetActorID()

	// create jwt client
	jwtclient, err := newJWTClient()
	if err != nil {
		return errors.Wrap(err, "Failed to create JWT client")
	}

	// get installation for the app
	installation, err := getOrgInstallationID(ctx, jwtclient, sourceOrgName)
	if err != nil {
		return errors.Wrap(err, "Failed to get installation for the app")
	}

	// create installation client
	sourceClient, err := h.ClientCreator.NewInstallationClient(installation)
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	// get custom repo roles for the source org
	customRepoRoles, err := getCustomRepoRolesForOrg(ctx, sourceClient, sourceOrgName)
	if err != nil {
		return errors.Wrap(err, "Failed to get custom repo roles for source org")
	}

	var roleName string

	for _, repoRole := range customRepoRoles.CustomRepoRoles {
		if repoRole.GetID() == actorID {
			roleName = repoRole.GetName()
		}
	}

	// get custom repo roles for the target org
	customRepoRoles, err = getCustomRepoRolesForOrg(ctx, client, orgName)
	if err != nil {
		return errors.Wrap(err, "Failed to get custom repo roles for target org")
	}

	for _, repoRole := range customRepoRoles.CustomRepoRoles {
		if repoRole.GetName() == roleName {
			actorID = repoRole.GetID()
			actor.ActorID = &actorID
			return nil
		}
	}

	return nil
}

func isManagedRuleset(event *RulesetEvent, ruleset *github.Ruleset, logger zerolog.Logger) bool {
	if ruleset.Name != event.Ruleset.Name {
		logger.Info().Msgf("Ruleset %s in the organization %s is not managed by this App.", event.Ruleset.Name, event.Organization.GetLogin())
		return false
	}
	logger.Info().Msgf("Ruleset %s in the organization %s is managed by this App.", event.Ruleset.Name, event.Organization.GetLogin())
	return true
}
