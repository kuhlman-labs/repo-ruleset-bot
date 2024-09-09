package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"

	"github.com/google/go-github/v63/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
)

// Constants for action types
const (
	ActionCreated = "created"
	ActionUpdated = "updated"
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
	Ruleset      *github.Ruleset      `json:"ruleset,omitempty"`
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

// Workflow represents a workflow.
type Workflow struct {
	RepositoryID interface{} `json:"repository_id"`
	Path         string      `json:"path"`
	Ref          string      `json:"ref"`
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

// RulesetHandler handles ruleset events.
type RulesetHandler struct {
	githubapp.ClientCreator
	RuleSet string
}

// Handles returns the list of event types handled by the RulesetHandler.
func (h *RulesetHandler) Handles() []string {
	return []string{"repository_ruleset", "installation"}
}

// Handle processes the event payload based on the event type.
func (h *RulesetHandler) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	switch eventType {
	case "repository_ruleset":
		return h.handleRepositoryRuleset(ctx, payload)
	case "installation":
		var event *github.InstallationEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return errors.Wrap(err, "failed to parse installation event payload")
		}
		return h.handleInstallation(ctx, event)
	default:
		return nil
	}
}

// handleRepositoryRuleset processes repository ruleset events.
func (h *RulesetHandler) handleRepositoryRuleset(ctx context.Context, payload []byte) error {
	var event *RulesetEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return errors.Wrap(err, "failed to parse repository ruleset event payload")
	}

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	switch event.Action {
	case ActionCreated:
		logger.Info().Msgf("Ruleset created for repository %s", event.Repository.GetName())
		return h.handleRulesetCreated(ctx, event, logger)
	case ActionUpdated:
		logger.Info().Msgf("Ruleset updated for repository %s", event.Repository.GetName())
		return h.handleRulesetUpdated(ctx, event, logger)
	case ActionDeleted:
		logger.Info().Msgf("Ruleset deleted for repository %s", event.Repository.GetName())
		return h.handleRulesetDeleted(ctx, event, logger)
	default:
		return nil
	}
}

// handleRulesetCreated handles the "created" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetCreated(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "failed to create installation client")
	}

	ruleset, err := readRulesetFromFile(h.RuleSet, client, event.Organization.GetName())
	if err != nil {
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	if !compareRulesets(ruleset, event.Ruleset) {
		logger.Info().Msgf("Ruleset does not match the ruleset in the rulesets.yml file")

		logChanges(event, logger)

		// Get Ruleset ID
		rulesetID := event.Ruleset.GetID()

		if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, event.Organization.GetName(), rulesetID, ruleset); err != nil {
			return errors.Wrap(err, "failed to update repository ruleset")
		}

		logger.Info().Msgf("Successfully reverted repository ruleset for repository %s", event.Repository.GetName())
		return nil
	}

	logger.Info().Msgf("Ruleset created for repository %s, no action required", event.Repository.GetName())
	return nil
}

// handleRulesetUpdated handles the "updated" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetUpdated(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	logger.Info().Msgf("Ruleset updated for repository %s", event.Repository.GetName())

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "failed to create installation client")
	}

	ruleset, err := readRulesetFromFile(h.RuleSet, client, event.Organization.GetName())
	if err != nil {
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	if !compareRulesets(ruleset, event.Ruleset) {
		logger.Info().Msgf("Ruleset does not match the ruleset in the rulesets.yml file")

		logChanges(event, logger)

		//get the ruleset ID
		rulesetID := event.Ruleset.GetID()

		if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, event.Organization.GetName(), rulesetID, ruleset); err != nil {
			return errors.Wrap(err, "failed to update repository ruleset")
		}

		logger.Info().Msgf("Successfully reverted repository ruleset for repository %s", event.Repository.GetName())
		return nil
	}

	return nil
}

// handleRulesetDeleted handles the "deleted" action for repository ruleset events.
func (h *RulesetHandler) handleRulesetDeleted(ctx context.Context, event *RulesetEvent, logger zerolog.Logger) error {
	logger.Info().Msgf("Ruleset deleted for repository %s", event.Repository.GetName())
	logger.Info().Msgf("Sender: %s", event.Sender.GetLogin())

	client, err := h.ClientCreator.NewInstallationClient(event.Installation.GetID())
	if err != nil {
		return errors.Wrap(err, "failed to create installation client")
	}

	ruleset, err := readRulesetFromFile(h.RuleSet, client, event.Organization.GetName())
	if err != nil {
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	// Get Ruleset ID
	rulesetID := event.Ruleset.GetID()

	if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, event.Organization.GetName(), rulesetID, ruleset); err != nil {
		return errors.Wrap(err, "failed to update repository ruleset")
	}

	logger.Info().Msgf("Successfully reverted repository ruleset for repository %s", event.Repository.GetName())
	return nil
}

// handleInstallation processes installation events.
func (h *RulesetHandler) handleInstallation(ctx context.Context, event *github.InstallationEvent) error {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	client, err := h.ClientCreator.NewInstallationClient(event.GetInstallation().GetID())
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create installation client")
		return errors.Wrap(err, "failed to create installation client")
	}

	ruleset, err := readRulesetFromFile(h.RuleSet, client, *event.Org.Login)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to read rulesets from file")
		return errors.Wrap(err, "failed to read rulesets from file")
	}

	if event.Org == nil || event.Org.Login == nil {
		err := errors.New("organization login is nil")
		logger.Error().Err(err).Msg("Organization login is nil")
		return err
	}

	orgRule, _, err := client.Organizations.CreateOrganizationRuleset(ctx, *event.Org.Login, ruleset)
	if err != nil {
		logger.Error().Err(err).Msgf("Failed to create organization ruleset for org %s", *event.Org.Login)
		return errors.Wrap(err, "failed to create organization ruleset")
	}

	logger.Info().Msgf("Successfully created organization ruleset for org %s with ID %d", *event.Org.Login, orgRule.GetID())
	return nil
}

// readRulesetFromFile reads the ruleset from a YAML file.
func readRulesetFromFile(filename string, client *github.Client, orgName string) (*github.Ruleset, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var ruleset github.Ruleset
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&ruleset); err != nil {
		return nil, err
	}

	// Check repository rules for type workflows
	for _, rule := range ruleset.Rules {
		if rule.Type == "workflows" {
			var workflow Workflow
			// Unmarshal the workflow parameters
			if err := json.Unmarshal(*rule.Parameters, &workflow); err != nil {
				return nil, err
			}
			// check if Workflow.RepositoryID is a string
			if reflect.TypeOf(workflow.RepositoryID).Kind() == reflect.String {
				repoName := workflow.RepositoryID.(string)
				repoID, err := getRepoID(context.Background(), client, orgName, repoName)
				if err != nil {
					return nil, errors.Wrap(err, "failed to get repository ID")
				}
				workflow.RepositoryID = repoID
				workflowJSON, err := json.Marshal(workflow)
				if err != nil {
					return nil, errors.Wrap(err, "failed to marshal workflow")
				}
				*rule.Parameters = json.RawMessage(workflowJSON)
			}
		}
	}

	return &ruleset, nil
}

// logChanges logs the changes in the ruleset event.
func logChanges(event *RulesetEvent, logger zerolog.Logger) {
	for _, rule := range event.Changes.Rules.Added {
		logger.Info().Msgf("Rule added: %s", rule.Type)
		logger.Info().Msgf("Sender: %s", event.Sender.GetLogin())
	}
}

// compareRulesets compares two rulesets.
func compareRulesets(ruleset1, ruleset2 *github.Ruleset) bool {
	ruleset1JSON, _ := json.Marshal(ruleset1)
	ruleset2JSON, _ := json.Marshal(ruleset2)

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
