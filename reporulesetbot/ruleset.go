package reporulesetbot

import (
	"context"
	"encoding/json"
	"os"
	"reflect"

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

func (h *RulesetHandler) processRuleset(ctx context.Context, ruleset *github.Ruleset, client *github.Client, orgName string, logger zerolog.Logger) error {
	for _, rule := range ruleset.Rules {
		if rule.Type == "workflows" {
			if err := processWorkflows(ctx, rule, client, orgName, logger); err != nil {
				return errors.Wrap(err, "Failed to process workflows.")
			}
		}
	}
	teamCounter := 0
	for _, bypassActor := range ruleset.BypassActors {
		if h.shouldProcessBypassActor(bypassActor) {
			if bypassActor.GetActorType() == "Team" {
				if teamCounter < len(h.Teams) {
					team := h.Teams[teamCounter]
					if err := processBypassActor(ctx, bypassActor, client, h.CustomRepoRoles, []string{team}, orgName, logger); err != nil {
						return errors.Wrap(err, "Failed to process bypass actors")
					}
					teamCounter++
				} else {
					logger.Warn().Msg("Not enough teams to pair with bypass actors.")
					break
				}
			} else {
				if err := processBypassActor(ctx, bypassActor, client, h.CustomRepoRoles, nil, orgName, logger); err != nil {
					return errors.Wrap(err, "Failed to process bypass actors")
				}
			}
		}
	}
	return nil
}

func (h *RulesetHandler) shouldProcessBypassActor(bypassActor *github.BypassActor) bool {
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

// processBypassActor processes a bypass actor.
func processBypassActor(ctx context.Context, actor *github.BypassActor, client *github.Client, repoRoles, teams []string, orgName string, logger zerolog.Logger) error {
	switch actor.GetActorType() {
	case "Team":
		return processTeamActor(ctx, actor, client, teams, orgName, logger)
	case "RepositoryRole":
		return processRepoRoleActor(ctx, actor, client, repoRoles, orgName, logger)
	default:
		return nil
	}
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
		} else {
			logger.Warn().Msgf("Repository role %s not found in organization %s.", repoRole, orgName)
		}
	}
	return nil
}

// compareConditions compares the conditions of two rulesets, returning true if they match.
func (h *RulesetHandler) compareConditions(rulesetConditions, eventConditions *github.RulesetConditions, logger zerolog.Logger) bool {

	if !reflect.DeepEqual(rulesetConditions, eventConditions) {
		logger.Info().Msgf("Conditions in the ruleset file do not match the conditions in the event.")
		return false
	}
	return true
}

func (h *RulesetHandler) isNameChanged(event *RulesetEvent, ruleset *github.Ruleset, logger zerolog.Logger) bool {
	if event.Changes != nil && event.Changes.Name.From == ruleset.Name {
		logger.Info().Msgf("Ruleset name has been changed from %s to %s. Reverting name change.", event.Changes.Name.From, event.Ruleset.Name)
		return true
	}
	return false
}

func (h *RulesetHandler) isEnforcementChanged(event *RulesetEvent, ruleset *github.Ruleset, logger zerolog.Logger) bool {
	if event.Changes != nil && event.Changes.Enforcement.From == ruleset.Enforcement {
		logger.Info().Msgf("Ruleset enforcement has been changed from %s to %s. Reverting enforcement change.", event.Changes.Enforcement.From, event.Ruleset.Enforcement)
		return true
	}
	return false
}

func (h *RulesetHandler) isManagedRuleset(event *RulesetEvent, ruleset *github.Ruleset, logger zerolog.Logger) bool {
	if ruleset.Name != event.Ruleset.Name {
		logger.Info().Msgf("Ruleset %s in the organization %s is not managed by the bot.", event.Ruleset.Name, event.Organization.GetLogin())
		return false
	}
	return true
}
