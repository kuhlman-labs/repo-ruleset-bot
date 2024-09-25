package reporulesetbot

import (
	"context"
	"encoding/json"
	"os"

	"github.com/google/go-github/v65/github"
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

// getRulesets returns the rulesets from the ruleset files.
func (h *RulesetHandler) getRulesets(ctx context.Context, client *github.Client, orgName string, logger zerolog.Logger) ([]*github.Ruleset, error) {
	var rulesets []*github.Ruleset

	files, err := getRulesetFiles("rulesets")
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get ruleset files")
	}

	for _, file := range files {
		ruleset, err := h.processRulesetFile(file, ctx, client, orgName, logger)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to process ruleset file %s", file)
		}
		rulesets = append(rulesets, ruleset)
	}
	return rulesets, nil
}

// processRulesetFile processes the ruleset from a given JSON file.
func (h *RulesetHandler) processRulesetFile(file string, ctx context.Context, client *github.Client, orgName string, logger zerolog.Logger) (*github.Ruleset, error) {
	logger.Info().Msgf("Processing ruleset file %s...", file)

	jsonData, err := os.ReadFile(file)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to read ruleset file %s.", file)
		return nil, errors.Wrap(err, "Failed to read ruleset file")
	}

	var ruleset *github.Ruleset
	if err := json.Unmarshal(jsonData, &ruleset); err != nil {
		logger.Error().Err(err).Msgf("Failed to unmarshal ruleset file %s.", file)
		return nil, errors.Wrap(err, "Failed to unmarshal ruleset file")
	}

	if err := h.processRuleset(ctx, ruleset, client, orgName, logger); err != nil {
		return nil, errors.Wrapf(err, "Failed to process ruleset file %s", file)
	}

	logger.Info().Msgf("Processed ruleset file %s.", file)

	return ruleset, nil
}

// processRuleset processes the ruleset.
func (h *RulesetHandler) processRuleset(ctx context.Context, ruleset *github.Ruleset, client *github.Client, orgName string, logger zerolog.Logger) error {
	sourceOrgName := ruleset.Source

	for _, rule := range ruleset.Rules {
		if rule.Type == "workflows" {
			if err := h.processWorkflows(ctx, rule, client, sourceOrgName, orgName, logger); err != nil {
				return errors.Wrapf(err, "Failed to process workflows in ruleset file: %s", ruleset.Name)
			}
		}
	}

	for _, bypassActor := range ruleset.BypassActors {
		if shouldProcessBypassActor(bypassActor) {
			switch bypassActor.GetActorType() {
			case "Team":
				if err := h.processTeamActor(ctx, client, bypassActor, sourceOrgName, orgName, logger); err != nil {
					return errors.Wrapf(err, "Failed to process team bypass actor with id %d in ruleset file: %s", bypassActor.GetActorID(), ruleset.Name)
				}
			case "RepositoryRole":
				if err := h.processRepoRoleActor(ctx, client, bypassActor, sourceOrgName, orgName, logger); err != nil {
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

// processWorkflows processes the workflows in a repository rule.
func (h *RulesetHandler) processWorkflows(ctx context.Context, rule *github.RepositoryRule, client *github.Client, sourceOrgName, orgName string, logger zerolog.Logger) error {
	var workflows Workflows
	if err := json.Unmarshal(*rule.Parameters, &workflows); err != nil {
		logger.Error().Err(err).Msg("Failed to unmarshal workflow parameters.")
		return errors.Wrap(err, "Failed to unmarshal workflow parameters")
	}

	for i, workflow := range workflows.Workflows {
		if err := h.updateWorkflowRepoID(ctx, &workflow, client, sourceOrgName, orgName, logger); err != nil {
			return errors.Wrapf(err, "Failed to update repository ID for workflow %s in ruleset file: %s", workflow.Path, orgName)
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
func (h *RulesetHandler) updateWorkflowRepoID(ctx context.Context, workflow *Workflow, client *github.Client, sourceOrgName, orgName string, logger zerolog.Logger) error {
	sourceClient, err := h.getSourceClient(ctx, sourceOrgName, logger)
	if err != nil {
		return err
	}

	repoName, err := getRepoName(ctx, sourceClient, workflow.RepositoryID)
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
func (h *RulesetHandler) processTeamActor(ctx context.Context, client *github.Client, actor *github.BypassActor, sourceOrgName, orgName string, logger zerolog.Logger) error {

	sourceClient, err := h.getSourceClient(ctx, sourceOrgName, logger)
	if err != nil {
		return err
	}

	orgID, err := getOrgID(ctx, sourceClient, sourceOrgName)
	if err != nil {
		return errors.Wrapf(err, "Failed to get org ID for the org %s", sourceOrgName)
	}

	teamID := actor.GetActorID()

	sourceTeam, err := getTeamByID(ctx, sourceClient, orgID, teamID)
	if err != nil {
		return errors.Wrapf(err, "Failed to get team with ID %d", teamID)
	}

	teamName := sourceTeam.GetSlug()

	newTeam, err := getTeamByName(ctx, client, orgName, teamName)
	if err != nil {
		return errors.Wrapf(err, "Failed to get team with name %s", teamName)
	}

	teamID = newTeam.GetID()

	actor.ActorID = &teamID

	return nil
}

// processRepoRoleActor processes a repository role actor.
func (h *RulesetHandler) processRepoRoleActor(ctx context.Context, client *github.Client, actor *github.BypassActor, sourceOrgName, orgName string, logger zerolog.Logger) error {
	actorID := actor.GetActorID()

	sourceClient, err := h.getSourceClient(ctx, sourceOrgName, logger)
	if err != nil {
		return err
	}

	customRepoRoles, err := getCustomRepoRolesForOrg(ctx, sourceClient, sourceOrgName)
	if err != nil {
		return errors.Wrapf(err, "Failed to get custom repo roles for org %s", sourceOrgName)
	}

	var roleName string

	for _, repoRole := range customRepoRoles.CustomRepoRoles {
		if repoRole.GetID() == actorID {
			roleName = repoRole.GetName()
		}
	}

	customRepoRoles, err = getCustomRepoRolesForOrg(ctx, client, orgName)
	if err != nil {
		return errors.Wrapf(err, "Failed to get custom repo roles for org %s", orgName)
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

// shouldProcessBypassActor returns true if the bypass actor should be processed.
func shouldProcessBypassActor(bypassActor *github.BypassActor) bool {
	actorID := bypassActor.GetActorID()
	return actorID != 0 && actorID > 5
}

// isManagedRuleset returns true if the ruleset is managed by this App.
func isManagedRuleset(event *RulesetEvent, ruleset *github.Ruleset, logger zerolog.Logger) bool {
	if event.Changes.Name.From != "" && ruleset.Name == event.Changes.Name.From {
		logger.Info().Msgf("Ruleset name was changed from %s to %s in the organization %s.", event.Changes.Name.From, event.Ruleset.Name, event.Organization.GetLogin())
		return true
	}

	if ruleset.Name != event.Ruleset.Name {
		logger.Info().Msgf("Ruleset %s does not match the event ruleset %s in the organization %s.", ruleset.Name, event.Ruleset.Name, event.Organization.GetLogin())
		return false
	}

	logger.Info().Msgf("Ruleset %s in the organization %s is managed by this App.", event.Ruleset.Name, event.Organization.GetLogin())
	return true
}

// getSourceClient creates a new installation client for the source organization.
func (h *RulesetHandler) getSourceClient(ctx context.Context, sourceOrgName string, logger zerolog.Logger) (*github.Client, error) {
	jwtclient, err := newJWTClient()
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create JWT client")
		return nil, errors.Wrap(err, "Failed to create JWT client")
	}

	installation, err := getOrgAppInstallationID(ctx, jwtclient, sourceOrgName)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get installation ID.")
		return nil, errors.Wrapf(err, "Failed to get installation ID for the org %s", sourceOrgName)
	}

	sourceClient, err := h.ClientCreator.NewInstallationClient(installation)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create installation client.")
		return nil, errors.Wrapf(err, "Failed to create installation client for ID %d", installation)
	}

	return sourceClient, nil
}
