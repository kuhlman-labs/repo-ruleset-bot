package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/go-github/v63/github"
	"github.com/gregjones/httpcache"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rcrowley/go-metrics"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
)

func main() {
	config, err := ReadConfig("config.yml")
	if err != nil {
		panic(err)
	}

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	zerolog.DefaultContextLogger = &logger

	metricsRegistry := metrics.DefaultRegistry

	cc, err := githubapp.NewDefaultCachingClientCreator(
		config.Github,
		githubapp.WithClientUserAgent("repo-ruleset-bot/1.0.0"),
		githubapp.WithClientTimeout(3*time.Second),
		githubapp.WithClientCaching(false, func() httpcache.Cache { return httpcache.NewMemoryCache() }),
		githubapp.WithClientMiddleware(
			githubapp.ClientMetrics(metricsRegistry),
		),
	)
	if err != nil {
		panic(err)
	}

	repoRulesetHandler := RulesetHandler{
		ClientCreator: cc,
		RuleSet:       config.RuleSet,
	}

	webhookHandler := githubapp.NewDefaultEventDispatcher(config.Github, &repoRulesetHandler)

	http.Handle(githubapp.DefaultWebhookRoute, webhookHandler)

	addr := fmt.Sprintf("%s:%d", config.Server.Address, config.Server.Port)
	logger.Info().Msgf("Starting server on %s...", addr)
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}

type Config struct {
	Server HTTPConfig       `yaml:"server"`
	Github githubapp.Config `yaml:"github"`

	RuleSet string `yaml:"ruleset"`
}

type HTTPConfig struct {
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

func ReadConfig(path string) (*Config, error) {
	var c Config

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed reading server config file: %s", path)
	}

	if err := yaml.UnmarshalStrict(bytes, &c); err != nil {
		return nil, errors.Wrap(err, "failed parsing configuration file")
	}

	return &c, nil
}

// RulesetHandler handles ruleset events.
type RulesetHandler struct {
	githubapp.ClientCreator
	RuleSet string
}

// Constants for action types
const (
	ActionCreated = "created"
	ActionUpdated = "edited"
	ActionDeleted = "deleted"
)

// RulesetEvent represents a GitHub ruleset event.
type RulesetEvent struct {
	Enterprise   *github.Enterprise   `json:"enterprise,omitempty"`
	Organization *github.Organization `json:"organization,omitempty"`
	Repository   *github.Repository   `json:"repository,omitempty"`
	Action       string               `json:"action,omitempty"`
	Installation *github.Installation `json:"installation,omitempty"`
	Sender       *github.User         `json:"sender,omitempty"`
	Ruleset      *github.Ruleset      `json:"repository_ruleset,omitempty"`
	Changes      *Changes             `json:"changes,omitempty"`
}

// Changes represents the changes in a ruleset event.
type Changes struct {
	Rules struct {
		Added []RuleChange `json:"added"`
	} `json:"rules"`
	Conditions struct {
		Added   []ConditionChange `json:"added"`
		Updated []ConditionChange `json:"updated"`
		Deleted []ConditionChange `json:"deleted"`
	} `json:"conditions"`
}

// RuleChange represents a change to a rule.
type RuleChange struct {
	Type       string     `json:"type"`
	Parameters RuleParams `json:"parameters"`
}

// RuleParams represents the parameters of a rule change.
type RuleParams struct {
	RequiredApprovingReviewCount     int                `json:"required_approving_review_count,omitempty"`
	DismissStaleReviewsOnPush        bool               `json:"dismiss_stale_reviews_on_push,omitempty"`
	RequireCodeOwnerReview           bool               `json:"require_code_owner_review,omitempty"`
	RequireLastPushApproval          bool               `json:"require_last_push_approval,omitempty"`
	RequiredReviewThreadResolution   bool               `json:"required_review_thread_resolution,omitempty"`
	StrictRequiredStatusChecksPolicy bool               `json:"strict_required_status_checks_policy,omitempty"`
	DoNotEnforceOnCreate             bool               `json:"do_not_enforce_on_create,omitempty"`
	RequiredStatusChecks             []StatusCheck      `json:"required_status_checks,omitempty"`
	Workflows                        []Workflow         `json:"workflows,omitempty"`
	CodeScanningTools                []CodeScanningTool `json:"code_scanning_tools,omitempty"`
	Operator                         string             `json:"operator,omitempty"`
	Pattern                          string             `json:"pattern,omitempty"`
	Negate                           bool               `json:"negate,omitempty"`
	Name                             string             `json:"name,omitempty"`
}

// StatusCheck represents a required status check.
type StatusCheck struct {
	Context string `json:"context"`
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

// CodeScanningTool represents a code scanning tool.
type CodeScanningTool struct {
	Tool                    string `json:"tool"`
	SecurityAlertsThreshold string `json:"security_alerts_threshold"`
	AlertsThreshold         string `json:"alerts_threshold"`
}

// ConditionChange represents a change to a condition.
type ConditionChange struct {
	RepositoryProperty struct {
		Exclude []interface{} `json:"exclude"`
		Include []struct {
			Name           string   `json:"name"`
			Source         string   `json:"source"`
			PropertyValues []string `json:"property_values"`
		} `json:"include"`
	} `json:"repository_property"`
	Condition struct {
		RefName struct {
			Exclude []interface{} `json:"exclude"`
			Include []string      `json:"include"`
		} `json:"ref_name"`
	} `json:"condition"`
	Changes struct {
		Include struct {
			From []string `json:"from"`
		} `json:"include"`
	} `json:"changes"`
	RepositoryName struct {
		Exclude []interface{} `json:"exclude"`
		Include []string      `json:"include"`
	} `json:"repository_name"`
}

// Handles returns the list of event types handled by the RulesetHandler.
func (h *RulesetHandler) Handles() []string {
	return []string{"repository_ruleset", "installation"}
}

// Handle processes the event payload based on the event type.
func (h *RulesetHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	switch eventType {
	case "repository_ruleset":

		var event *RulesetEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return errors.Wrap(err, "failed to parse repository ruleset event payload")
		}

		logger.Info().Msgf("Repository ruleset event received: %s", event.Action)

		return h.handleRepositoryRuleset(ctx, event)
	case "installation":

		var event *github.InstallationEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return errors.Wrap(err, "failed to parse installation event payload")
		}

		logger.Info().Msgf("Installation event received: %s", event.GetAction())

		return h.handleInstallation(ctx, event)
	default:
		return nil
	}
}

// handleRepositoryRuleset processes organization ruleset events.
func (h *RulesetHandler) handleRepositoryRuleset(ctx context.Context, event *RulesetEvent) error {

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	switch event.Action {
	case ActionCreated:
		return h.handleRulesetCreated(event, logger)
	case ActionUpdated:
		return h.handleRulesetUpdated(ctx, event, logger)
	case ActionDeleted:
		return h.handleRulesetDeleted(ctx, event, logger)
	default:
		return nil
	}
}

// handleRulesetCreated handles the "created" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetCreated(event *RulesetEvent, logger zerolog.Logger) error {

	logger.Info().Msgf("Ruleset has been created in the organization %s by %s", event.Organization.GetLogin(), event.Sender.GetLogin())

	return nil
}

// handleRulesetUpdated handles the "updated" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetUpdated(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {

	logger.Info().Msgf("Ruleset %s has been updated in the organization %s by %s", event.Ruleset.Name, event.Organization.GetLogin(), event.Sender.GetLogin())

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "failed to create installation client")
	}

	ruleset, err := readRulesetFromFile(h.RuleSet, client, event.Organization.GetLogin())
	if err != nil {
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	// check if the ruleset is the same as the ruleset in the ruleset.json file
	if !isSameRuleset(ruleset, event) {
		logger.Info().Msgf("Ruleset is not managed by the bot")
		return nil
	}

	rulesetID := event.Ruleset.GetID()

	if !compareRulesets(ruleset, event.Ruleset) {
		logger.Info().Msgf("Ruleset does not match the ruleset in the ruleset.json file, reverting changes")

		if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, event.Organization.GetLogin(), rulesetID, ruleset); err != nil {
			return errors.Wrap(err, "Failed to update repository ruleset")
		}

		logger.Info().Msgf("Successfully reverted repository ruleset for organization %s", event.Organization.GetLogin())
		return nil
	}

	return nil
}

// handleRulesetDeleted handles the "deleted" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetDeleted(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "failed to create installation client")
	}

	orgName := event.Organization.GetLogin()

	ruleset, err := readRulesetFromFile(h.RuleSet, client, orgName)
	if err != nil {
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	// check if the ruleset is the same as the ruleset in the ruleset.json file
	if !isSameRuleset(ruleset, event) {
		logger.Info().Msgf("Ruleset %s has been deleted in the organization %s by %s", event.Ruleset.Name, orgName, event.Sender.GetLogin())
		logger.Info().Msgf("Ruleset is not managed by the bot")
		return nil
	}

	logger.Info().Msgf("Ruleset %s has been deleted in the organization %s by %s", event.Ruleset.Name, orgName, event.Sender.GetLogin())
	logger.Info().Msgf("Redeploying ruleset %s Ruleset Is Managed By The Bot", event.Ruleset.Name)

	if _, _, err := client.Organizations.CreateOrganizationRuleset(ctx, orgName, ruleset); err != nil {
		return errors.Wrap(err, "Failed to redeploy repository ruleset")
	}

	logger.Info().Msgf("Successfully redeployed repository ruleset for organization %s", orgName)
	return nil
}

// handleInstallation processes installation events.
func (h *RulesetHandler) handleInstallation(ctx context.Context, event *github.InstallationEvent) error {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Only process installation events for new installations
	if event.GetAction() != "created" {
		return nil
	}

	installation := event.GetInstallation()
	if installation == nil {
		err := errors.New("installation is nil")
		logger.Error().Err(err).Msg("Installation is nil")
		return err
	}

	logger.Info().Msgf("Installation created for installation ID %d", event.GetInstallation().GetID())

	client, err := h.ClientCreator.NewInstallationClient(installation.GetID())
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create installation client")
		return errors.Wrap(err, "failed to create installation client")
	}

	orgName := event.Installation.Account.GetLogin()

	ruleset, err := readRulesetFromFile(h.RuleSet, client, orgName)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to read rulesets from file")
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	orgRule, _, err := client.Organizations.CreateOrganizationRuleset(ctx, orgName, ruleset)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to create organization ruleset for org %s", orgName)
		return errors.Wrap(err, "failed to create organization ruleset")
	}

	logger.Info().Msgf("Successfully created organization ruleset for org %s with ID %d", orgName, orgRule.GetID())

	return nil
}

// readRulesetFromFile reads the ruleset from a JSON file.
func readRulesetFromFile(filename string, client *github.Client, orgName string) (*github.Ruleset, error) {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	file, err := os.Open(filename)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to open ruleset file %s", filename)
		return nil, err
	}
	defer file.Close()

	jsonData, err := os.ReadFile(filename)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to read ruleset file %s", filename)
		return nil, err
	}

	var ruleset *github.Ruleset
	err = json.Unmarshal(jsonData, &ruleset)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to unmarshal ruleset file %s", filename)
		return nil, err
	}

	for _, rule := range ruleset.Rules {
		if rule.Type == "workflows" {
			err := processWorkflows(rule, client, orgName, logger)
			if err != nil {
				return nil, err
			}
		}
	}

	sourceOrgName := ruleset.Source
	org, _, err := client.Organizations.Get(context.Background(), orgName)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to get organization %s", orgName)
		return nil, err
	}

	orgID := org.GetID()

	for _, bypassActor := range ruleset.BypassActors {
		actorID := bypassActor.GetActorID()
		// Skip if bypass actor ID is 1, 2, 3, 4, or 5
		if actorID == 1 || actorID == 2 || actorID == 3 || actorID == 4 || actorID == 5 {
			continue
		}

		if actorID != 0 {
			err := processBypassActor(bypassActor, client, orgID, orgName, sourceOrgName, logger)
			if err != nil {
				return nil, err
			}
		}
	}

	return ruleset, nil
}

// compareRulesets compares two rulesets.
func compareRulesets(ruleset1, ruleset2 *github.Ruleset) bool {

	// Remove the ID from the rulesets
	ruleset1.ID = nil
	ruleset2.ID = nil

	// Unmarshal the rulesets to JSON
	ruleset1JSON, err := json.Marshal(ruleset1)
	if err != nil {
		return false
	}

	ruleset2JSON, err := json.Marshal(ruleset2)
	if err != nil {
		return false
	}

	// Compare the rulesets
	return string(ruleset1JSON) == string(ruleset2JSON)

}

// getRepoID returns the repository ID from a given repository name.
func getRepoID(ctx context.Context, client *github.Client, owner, repo string) (int64, error) {
	repository, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return 0, errors.Wrap(err, "failed to get repository")
	}

	return repository.GetID(), nil
}

// getRepoName returns the repository name from a given repository ID.
func getRepoName(ctx context.Context, client *github.Client, repoID int64) (string, error) {
	repository, _, err := client.Repositories.GetByID(ctx, repoID)
	if err != nil {
		return "", errors.Wrap(err, "failed to get repository")
	}

	return repository.GetName(), nil
}

func processWorkflows(rule *github.RepositoryRule, client *github.Client, orgName string, logger zerolog.Logger) error {
	logger.Info().Msgf("Workflow rule found in ruleset")

	var workflows Workflows
	err := json.Unmarshal(*rule.Parameters, &workflows)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to unmarshal workflow parameters")
		return err
	}

	for i, workflow := range workflows.Workflows {

		// Get the repository name from the repository ID
		repoName, err := getRepoName(context.Background(), client, workflow.RepositoryID)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to get repository name")
			return err
		}

		// Look up the repository ID for repo in originating organization
		newRepoID, err := getRepoID(context.Background(), client, orgName, repoName)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to get repository ID")
			return err
		}

		// Update the repository ID in the workflow
		workflows.Workflows[i].RepositoryID = newRepoID
	}

	// Marshal the updated workflows struct back to JSON
	updatedWorkflowsJSON, err := json.Marshal(workflows)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to marshal updated workflows")
		return err
	}

	// Update the rule parameters with the new JSON data
	*rule.Parameters = updatedWorkflowsJSON

	return nil
}

func isSameRuleset(ruleset *github.Ruleset, event *RulesetEvent) bool {
	return ruleset.Name == event.Ruleset.Name
}

func getTeamByID(ctx context.Context, client *github.Client, orgID, teamID int64) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamByID(ctx, orgID, teamID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get team")
	}

	return team, nil
}

func getTeamByName(ctx context.Context, client *github.Client, orgName, teamName string) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamBySlug(ctx, orgName, teamName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get team")
	}

	return team, nil
}

//TODO: Add a function to get the CustomRole ID for an Organization

func getCustomRoleForOrg(ctx context.Context, client *github.Client, orgName string) (*github.OrganizationCustomRoles, error) {
	customRole, _, err := client.Organizations.ListRoles(ctx, orgName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get custom role")
	}

	return customRole, nil
}

// processBypassActor processes the bypass actor in the ruleset. This will loop through the bypass actors in the ruleset and
// update the actor ID to the new actor ID in the originating organization.
func processBypassActor(actor *github.BypassActor, client *github.Client, orgID int64, orgName string, sourceOrgName string, logger zerolog.Logger) error {
	if actor.GetActorType() == "Team" {
		teamID := actor.GetActorID()
		team, err := getTeamByID(context.Background(), client, orgID, teamID)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to get team with ID %d", teamID)
			return err
		}

		teamName := team.GetSlug()

		newTeam, err := getTeamByName(context.Background(), client, orgName, teamName)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to get team with name %s", teamName)
			return err
		}

		newTeamID := newTeam.GetID()

		actor.ActorID = &newTeamID

	} else if actor.GetActorType() == "RepositoryRole" {

		actorID := actor.GetActorID()

		sourceCustomRole, err := getCustomRoleForOrg(context.Background(), client, sourceOrgName)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to get custom role for organization %s", sourceOrgName)
			return err
		}

		var roleName string

		for _, role := range sourceCustomRole.CustomRepoRoles {
			if role.GetID() == actorID {
				roleName = role.GetName()
			}
		}

		customRole, err := getCustomRoleForOrg(context.Background(), client, orgName)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to get custom role for organization %s", orgName)
			return err
		}

		for _, role := range customRole.CustomRepoRoles {
			if role.GetName() == roleName {
				newRoleID := role.GetID()
				actor.ActorID = &newRoleID
			}
		}

	}

	return nil
}
