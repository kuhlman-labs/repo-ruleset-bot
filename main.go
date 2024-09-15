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
		ClientCreator:   cc,
		RuleSet:         config.RuleSet,
		CustomRepoRoles: config.CustomRepoRoles,
		Teams:           config.Teams,
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

	RuleSet         string   `yaml:"ruleset"`
	CustomRepoRoles []string `yaml:"custom_repo_roles"`
	Teams           []string `yaml:"teams"`
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
	RuleSet         string
	CustomRepoRoles []string
	Teams           []string
}

// Constants for action types
const (
	ActionCreated = "created"
	ActionEdited  = "edited"
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
	Name struct {
		From string `json:"from,omitempty"`
	} `json:"name,omitempty"`
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
	case ActionEdited:
		return h.handleRulesetEdited(ctx, event, logger)
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

// handleRulesetEditedhandles the "edited" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetEdited(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {

	logger.Info().Msgf("Ruleset %s has been edited in the organization %s by %s", event.Ruleset.Name, event.Organization.GetLogin(), event.Sender.GetLogin())

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "failed to create installation client")
	}

	ruleset, err := h.readRulesetFromFile(h.RuleSet, client, event.Organization.GetLogin())
	if err != nil {
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	rulesetID := event.Ruleset.GetID()

	//check if name of the ruleset has been changed
	if event.Changes.Name.From != "" && event.Changes.Name.From == ruleset.Name {
		logger.Info().Msgf("Ruleset name has been changed from %s to %s. Reverting name change.", event.Changes.Name.From, event.Ruleset.Name)
		if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, event.Organization.GetLogin(), rulesetID, ruleset); err != nil {
			return errors.Wrap(err, "Failed to update repository ruleset")
		}
		logger.Info().Msgf("Successfully updated repository ruleset for organization %s", event.Organization.GetLogin())
	}

	// check if the ruleset is the same as the ruleset in the ruleset.json file
	if ruleset.Name != event.Ruleset.Name {
		logger.Info().Msgf("Ruleset is not managed by the bot")
		return nil
	}

	if !compareRulesets(ruleset, event.Ruleset) {
		logger.Info().Msgf("Ruleset does not match the ruleset set in the ruleset file, reverting changes")

		if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, event.Organization.GetLogin(), rulesetID, ruleset); err != nil {
			return errors.Wrap(err, "Failed to update repository ruleset")
		}

		logger.Info().Msgf("Successfully updated repository ruleset for organization %s", event.Organization.GetLogin())
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

	ruleset, err := h.readRulesetFromFile(h.RuleSet, client, orgName)
	if err != nil {
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	// check if the ruleset is the same as the ruleset in the ruleset.json file
	if ruleset.Name != event.Ruleset.Name {
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

	ruleset, err := h.readRulesetFromFile(h.RuleSet, client, orgName)
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
func (h *RulesetHandler) readRulesetFromFile(filename string, client *github.Client, orgName string) (*github.Ruleset, error) {
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

	for _, bypassActor := range ruleset.BypassActors {
		actorID := bypassActor.GetActorID()
		if actorID == 1 || actorID == 2 || actorID == 3 || actorID == 4 || actorID == 5 {
			continue
		}

		if actorID != 0 {
			err := processBypassActor(bypassActor, client, h.CustomRepoRoles, h.Teams, orgName, logger)
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

func getTeamByName(ctx context.Context, client *github.Client, orgName, teamName string) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamBySlug(ctx, orgName, teamName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get team")
	}

	return team, nil
}

func getCustomRepoRolesForOrg(ctx context.Context, client *github.Client, orgName string) (*github.OrganizationCustomRepoRoles, error) {
	customRepoRoles, _, err := client.Organizations.ListCustomRepoRoles(ctx, orgName)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get custom repository roles for organization: %s", orgName)
	}

	return customRepoRoles, nil
}

// processBypassActor processes the bypass actor in the ruleset. This will loop through the bypass actors in the ruleset and
// update the actor ID to the new actor ID in the originating organization.
func processBypassActor(actor *github.BypassActor, client *github.Client, repoRoles, teams []string, orgName string, logger zerolog.Logger) error {
	if actor.GetActorType() == "Team" {
		for _, team := range teams {
			newTeam, err := getTeamByName(context.Background(), client, orgName, team)
			if err != nil {
				logger.Error().Err(err).Msgf("Failed to get team with name %s", team)
				return err
			}

			newRoleID := newTeam.GetID()

			actor.ActorID = &newRoleID
		}
	} else if actor.GetActorType() == "RepositoryRole" {

		for _, repoRole := range repoRoles {

			customRepoRoles, err := getCustomRepoRolesForOrg(context.Background(), client, orgName)
			if err != nil {
				logger.Error().Err(err).Msgf("Failed to get custom repo role for organization: %s", orgName)
				return err
			}

			var newRoleID int64

			for _, role := range customRepoRoles.CustomRepoRoles {
				if role.GetName() == repoRole {
					newRoleID = role.GetID()
				}
			}
			actor.ActorID = &newRoleID
		}
	}

	return nil
}
