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

	return ruleset, nil
}

// processRuleset processes the ruleset.
func (h *RulesetHandler) processRuleset(ctx context.Context, ruleset *github.Ruleset, client *github.Client, orgName string, logger zerolog.Logger) error {
	for _, rule := range ruleset.Rules {
		if rule.Type == "workflows" {
			if err := processWorkflows(ctx, rule, client, orgName, logger); err != nil {
				return errors.Wrap(err, "Failed to process workflows.")
			}
		}
	}

	teamCounter := 0
	customRepoRoleCounter := 0
	for _, bypassActor := range ruleset.BypassActors {
		if shouldProcessBypassActor(bypassActor) {
			switch bypassActor.GetActorType() {
			case "Team":
				if teamCounter < len(h.Teams) {
					team := h.Teams[teamCounter]
					if err := processTeamActor(ctx, bypassActor, client, []string{team}, orgName, logger); err != nil {
						return errors.Wrap(err, "Failed to process team bypass actors")
					}
					teamCounter++
				} else {
					logger.Warn().Msg("Not enough teams to pair with bypass actors.")
					break
				}
			case "RepositoryRole":
				if customRepoRoleCounter < len(h.CustomRepoRoles) {
					repoRole := h.CustomRepoRoles[customRepoRoleCounter]
					if err := processRepoRoleActor(ctx, bypassActor, client, []string{repoRole}, orgName, logger); err != nil {
						return errors.Wrap(err, "Failed to process repository role bypass actors")
					}
					customRepoRoleCounter++
				} else {
					logger.Warn().Msg("Not enough custom repository roles to pair with bypass actors.")
					break
				}
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
		logger.Error().Err(err).Msg("Failed to get repository name.")
		return errors.Wrap(err, "Failed to get repository name")
	}

	newRepoID, err := getRepoID(ctx, client, orgName, repoName)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get repository ID.")
		return errors.Wrap(err, "Failed to get repository ID")
	}

	workflow.RepositoryID = newRepoID
	return nil
}

// processTeamActor processes a team actor.
func processTeamActor(ctx context.Context, actor *github.BypassActor, client *github.Client, teams []string, orgName string, logger zerolog.Logger) error {
	if len(teams) == 0 {
		return errors.New("No teams provided")
	}

	teamName := teams[0] // Pair the first team with the bypass actor
	team, err := getTeamByName(ctx, client, orgName, teamName)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to get team with name %s.", teamName)
		return err
	}

	teamID := team.GetID()
	actor.ActorID = &teamID
	return nil
}

// processRepoRoleActor processes a repository role actor.
func processRepoRoleActor(ctx context.Context, actor *github.BypassActor, client *github.Client, repoRoles []string, orgName string, logger zerolog.Logger) error {
	if len(repoRoles) == 0 {
		return errors.New("No repository roles provided")
	}

	customRepoRoles, err := getCustomRepoRolesForOrg(ctx, client, orgName)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to get custom repo roles for organization: %s.", orgName)
		return err
	}

	roleIDMap := make(map[string]int64)
	for _, role := range customRepoRoles.CustomRepoRoles {
		roleIDMap[role.GetName()] = role.GetID()
	}

	for _, repoRole := range repoRoles {
		if roleID, exists := roleIDMap[repoRole]; exists {
			actor.ActorID = &roleID
			return nil // Process only one custom role per bypass actor
		} else {
			logger.Warn().Msgf("Repository role %s not found in organization %s.", repoRole, orgName)
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
