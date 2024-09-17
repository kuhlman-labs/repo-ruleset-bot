package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
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
		Logger:          logger,
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
		logger.Fatal().Err(err).Msg("Failed to start server.")
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

// ReadConfig reads and parses the configuration file.
func ReadConfig(path string) (*Config, error) {
	var config Config

	// Read the file
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to read configuration file: %s", path)
	}

	// Unmarshal the YAML content
	if err := yaml.UnmarshalStrict(bytes, &config); err != nil {
		return nil, errors.Wrap(err, "Failed to parse configuration file")
	}

	// Validate the configuration
	if err := validateConfig(&config); err != nil {
		return nil, errors.Wrap(err, "Invalid configuration")
	}

	return &config, nil
}

// validateConfig validates the configuration fields.
func validateConfig(config *Config) error {
	requiredFields := map[string]interface{}{
		"Server Address":            config.Server.Address,
		"Server Port":               config.Server.Port,
		"Ruleset":                   config.RuleSet,
		"GitHub App ID":             config.Github.App.IntegrationID,
		"GitHub App private key":    config.Github.App.PrivateKey,
		"GitHub App webhook secret": config.Github.App.WebhookSecret,
		"GitHub v3 API URL":         config.Github.V3APIURL,
	}

	for field, value := range requiredFields {
		if isEmpty(value) {
			return errors.New(fmt.Sprintf("%s field is required to be set in the config.yml file.", field))
		}
	}

	return nil
}

// isEmpty checks if a value is considered empty.
func isEmpty(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return v == ""
	case int:
		return v == 0
	case []string:
		return len(v) == 0
	default:
		return value == nil
	}
}

// RulesetHandler handles ruleset events.
type RulesetHandler struct {
	githubapp.ClientCreator
	zerolog.Logger
	RuleSet         string
	CustomRepoRoles []string
	Teams           []string
}

// Constants for action and event types
const (
	ActionCreated              = "created"
	ActionEdited               = "edited"
	ActionDeleted              = "deleted"
	EventTypeRepositoryRuleset = "repository_ruleset"
	EventTypeInstallation      = "installation"
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

// Handles returns the list of event types handled by the RulesetHandler.
func (h *RulesetHandler) Handles() []string {
	return []string{"repository_ruleset", "installation"}
}

// Handle processes the event payload based on the event type.
func (h *RulesetHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {

	logger := h.Logger

	switch eventType {
	case EventTypeRepositoryRuleset:
		return h.handleRepositoryRulesetEvent(ctx, payload, logger)
	case EventTypeInstallation:
		return h.handleInstallationEvent(ctx, payload, logger)
	default:
		logger.Warn().Msgf("Unhandled event type: %s.", eventType)
		return nil
	}
}

// handleRepositoryRulesetEvent handles repository ruleset events.
func (h *RulesetHandler) handleRepositoryRulesetEvent(ctx context.Context, payload []byte, logger zerolog.Logger) error {
	var event *RulesetEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		logger.Error().Err(err).Msg("Failed to parse repository ruleset event payload.")
		return errors.Wrap(err, "Failed to parse repository ruleset event payload")
	}

	logger.Info().Msgf("Repository ruleset event received for the organization %s: %s.", event.Organization.GetLogin(), event.Action)
	return h.handleRepositoryRuleset(ctx, event, logger)
}

// handleInstallationEvent handles installation events.
func (h *RulesetHandler) handleInstallationEvent(ctx context.Context, payload []byte, logger zerolog.Logger) error {
	var event *github.InstallationEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		logger.Error().Err(err).Msg("Failed to parse installation event payload.")
		return errors.Wrap(err, "Failed to parse installation event payload")
	}

	logger.Info().Msgf("Installation event received for the organization %s: %s.", event.Installation.Account.GetLogin(), event.GetAction())
	return h.handleInstallation(ctx, event, logger)
}

// handleRepositoryRuleset processes organization ruleset events.
func (h *RulesetHandler) handleRepositoryRuleset(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	switch event.Action {
	case ActionCreated:
		return h.handleRulesetCreated(event, logger)
	case ActionEdited:
		return h.handleRulesetEdited(ctx, event, logger)
	case ActionDeleted:
		return h.handleRulesetDeleted(ctx, event, logger)
	default:
		logger.Warn().Msgf("Unhandled action type: %s.", event.Action)
		return nil
	}
}

// handleRulesetCreated handles the "created" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetCreated(event *RulesetEvent, logger zerolog.Logger) error {
	logger.Info().Msgf("Ruleset has been created in the organization %s by %s.", event.Organization.GetLogin(), event.Sender.GetLogin())
	return nil
}

// handleRulesetEditedhandles the "edited" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetEdited(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	logger.Info().Msgf("Ruleset %s has been edited in the organization %s by %s.", event.Ruleset.Name, event.Organization.GetLogin(), event.Sender.GetLogin())

	orgName := event.Organization.GetLogin()

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	ruleset, err := h.readRulesetFromFile(h.RuleSet, ctx, client, orgName, logger)
	if err != nil {
		return errors.Wrap(err, "Failed to read ruleset from file")
	}

	rulesetID := event.Ruleset.GetID()

	// Check if the ruleset needs to be updated
	if h.isNameChanged(event, ruleset, logger) || h.isEnforcementChanged(event, ruleset, logger) || !h.compareRulesets(ruleset, event.Ruleset, logger) {
		logger.Info().Msgf("Updating ruleset %s for organization %s.", event.Ruleset.Name, event.Organization.GetLogin())
		h.editRuleset(ctx, client, orgName, rulesetID, ruleset, logger)
		return nil
	}

	if !h.isManagedRuleset(event, ruleset, logger) {
		return nil
	}

	logger.Info().Msgf("Ruleset %s in the organization %s matches the ruleset set in the ruleset file.", event.Ruleset.Name, event.Organization.GetLogin())
	return nil
}

// compareRulesets compares two rulesets, returning true if they match.
func (h *RulesetHandler) compareRulesets(ruleset, event *github.Ruleset, logger zerolog.Logger) bool {
	if ruleset == nil || event == nil {
		return false
	}

	if !h.compareRules(ruleset.Rules, event.Rules, logger) {
		return false
	}

	if !h.compareConditions(ruleset.Conditions, event.Conditions, logger) {
		return false
	}

	return true
}

// compareRules compares the rules of two rulesets, returning true if they match.
func (h *RulesetHandler) compareRules(rulesetRules, eventRules []*github.RepositoryRule, logger zerolog.Logger) bool {
	if len(rulesetRules) != len(eventRules) {
		logger.Info().Msgf("Number of rules in the ruleset file does not match the number of rules in the event.")
		return false
	}

	rulesetRuleTypes := make(map[string]struct{}, len(rulesetRules))
	for _, rule := range rulesetRules {
		rulesetRuleTypes[rule.Type] = struct{}{}
	}

	for _, eventRule := range eventRules {
		if _, exists := rulesetRuleTypes[eventRule.Type]; !exists {
			logger.Info().Msgf("Rule type %s in the ruleset file was not found in the event.", eventRule.Type)
			return false
		}
	}

	return true
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

// handleRulesetDeleted handles the "deleted" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetDeleted(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	orgName := event.Organization.GetLogin()
	logger.Info().Msgf("Ruleset %s has been deleted in the organization %s by %s.", event.Ruleset.Name, orgName, event.Sender.GetLogin())

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	ruleset, err := h.readRulesetFromFile(h.RuleSet, ctx, client, orgName, logger)
	if err != nil {
		return errors.Wrap(err, "Failed to read rulesets from file")
	}

	if !h.isManagedRuleset(event, ruleset, logger) {
		return nil
	}

	logger.Info().Msgf("Recreating ruleset %s in organization %s.", event.Ruleset.Name, orgName)

	if err := h.createRuleset(ctx, client, orgName, ruleset, logger); err != nil {
		return err
	}

	return nil
}

func (h *RulesetHandler) isManagedRuleset(event *RulesetEvent, ruleset *github.Ruleset, logger zerolog.Logger) bool {
	if ruleset.Name != event.Ruleset.Name {
		logger.Info().Msgf("Ruleset %s in the organization %s is not managed by the bot.", event.Ruleset.Name, event.Organization.GetLogin())
		return false
	}
	return true
}

func (h *RulesetHandler) createRuleset(ctx context.Context, client *github.Client, orgName string, ruleset *github.Ruleset, logger zerolog.Logger) error {
	if _, _, err := client.Organizations.CreateOrganizationRuleset(ctx, orgName, ruleset); err != nil {
		return errors.Wrap(err, "Failed to deploy repository ruleset")
	}
	logger.Info().Msgf("Successfully created the %s ruleset for organization %s.", ruleset.Name, orgName)
	return nil
}

func (h *RulesetHandler) editRuleset(ctx context.Context, client *github.Client, orgName string, rulesetID int64, ruleset *github.Ruleset, logger zerolog.Logger) error {
	if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, orgName, rulesetID, ruleset); err != nil {
		return errors.Wrap(err, "Failed to update repository ruleset")
	}
	logger.Info().Msgf("Successfully updated the %s ruleset for organization %s.", ruleset.Name, orgName)
	return nil
}

// handleInstallation processes installation events.
func (h *RulesetHandler) handleInstallation(ctx context.Context, event *github.InstallationEvent, logger zerolog.Logger) error {

	installationID := event.GetInstallation().GetID()
	orgName := event.Installation.Account.GetLogin()
	action := event.GetAction()
	appName := event.GetInstallation().GetAppSlug()

	logger.Info().Msgf("Application %s installed in the organization %s.", appName, orgName)

	if action != "created" {
		return nil
	}

	client, err := h.ClientCreator.NewInstallationClient(installationID)
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	ruleset, err := h.readRulesetFromFile(h.RuleSet, ctx, client, orgName, logger)
	if err != nil {
		return errors.Wrap(err, "Failed to read rulesets from file")
	}

	h.createRuleset(ctx, client, orgName, ruleset, logger)

	return nil
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

	for _, bypassActor := range ruleset.BypassActors {
		if h.shouldProcessBypassActor(bypassActor) {
			if err := processBypassActor(ctx, bypassActor, client, h.CustomRepoRoles, h.Teams, orgName, logger); err != nil {
				return errors.Wrap(err, "Failed to process bypass actors")
			}
		}
	}
	return nil
}

func (h *RulesetHandler) shouldProcessBypassActor(bypassActor *github.BypassActor) bool {
	actorID := bypassActor.GetActorID()
	return actorID != 0 && actorID > 5
}

// getRepoID returns the repository ID from a given repository name.
func getRepoID(ctx context.Context, client *github.Client, owner, repo string) (int64, error) {
	repository, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get repository")
	}

	return repository.GetID(), nil
}

// getRepoName returns the repository name from a given repository ID.
func getRepoName(ctx context.Context, client *github.Client, repoID int64) (string, error) {
	repository, _, err := client.Repositories.GetByID(ctx, repoID)
	if err != nil {
		return "", errors.Wrap(err, "Failed to get repository")
	}

	return repository.GetName(), nil
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

// getTeamByName returns the team by its name.
func getTeamByName(ctx context.Context, client *github.Client, orgName, teamName string) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamBySlug(ctx, orgName, teamName)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get team")
	}
	return team, nil
}

// getCustomRepoRolesForOrg returns the custom repository roles for an organization.
func getCustomRepoRolesForOrg(ctx context.Context, client *github.Client, orgName string) (*github.OrganizationCustomRepoRoles, error) {
	customRepoRoles, _, err := client.Organizations.ListCustomRepoRoles(ctx, orgName)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get custom repository roles for organization: %s", orgName)
	}
	return customRepoRoles, nil
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
	for _, teamName := range teams {
		team, err := getTeamByName(ctx, client, orgName, teamName)
		if err != nil {
			logger.Error().Err(err).Msgf("Failed to get team with name %s.", teamName)
			return err
		}
		teamID := team.GetID()
		actor.ActorID = &teamID
	}
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
