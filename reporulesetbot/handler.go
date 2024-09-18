package reporulesetbot

import (
	"context"
	"encoding/json"

	"github.com/google/go-github/v63/github"
	"github.com/palantir/go-githubapp/githubapp"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

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
	orgName := event.Organization.GetLogin()
	eventInstallationID := event.Installation.GetID()
	eventSender := event.Sender.GetLogin()
	rulesetID := event.Ruleset.GetID()
	rulesetName := event.Ruleset.Name

	jwtclient, err := newJWTClient()
	if err != nil {
		return errors.Wrap(err, "Failed to create JWT client")
	}

	appName, err := getAuthenticatedApp(ctx, jwtclient)
	if err != nil {
		return errors.Wrap(err, "Failed to get authenticated app")
	}

	appName = appName + "[bot]"

	client, err := h.ClientCreator.NewInstallationClient(eventInstallationID)
	if err != nil {
		return errors.Wrap(err, "Failed to create installation client")
	}

	if eventSender == appName {
		logger.Info().Msgf("Ruleset %s in the organization %s was edited by %s.", rulesetName, orgName, appName)
		return nil
	} else {
		logger.Info().Msgf("Ruleset %s in the organization %s was edited by the user %s.", rulesetName, orgName, eventSender)
		ruleset, err := h.readRulesetFromFile(h.RuleSet, ctx, client, orgName, logger)
		if err != nil {
			return errors.Wrap(err, "Failed to read ruleset from file")
		}
		if !isManagedRuleset(event, ruleset, logger) {
			return nil
		}
		editRuleset(ctx, client, orgName, rulesetID, ruleset, logger)
		return nil
	}
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

	if !isManagedRuleset(event, ruleset, logger) {
		return nil
	}

	logger.Info().Msgf("Recreating ruleset %s in organization %s.", event.Ruleset.Name, orgName)

	if err := createRuleset(ctx, client, orgName, ruleset, logger); err != nil {
		return err
	}

	return nil
}

// handleInstallation processes installation events.
func (h *RulesetHandler) handleInstallation(ctx context.Context, event *github.InstallationEvent, logger zerolog.Logger) error {
	installationID := event.GetInstallation().GetID()
	orgName := event.Installation.Account.GetLogin()
	action := event.GetAction()
	appName := event.GetInstallation().GetAppSlug()

	logger.Info().Msgf("Application %s was installed in the organization %s.", appName, orgName)

	if action != ActionCreated {
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

	createRuleset(ctx, client, orgName, ruleset, logger)

	return nil
}
