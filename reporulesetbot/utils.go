package reporulesetbot

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v63/github"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// createRuleset creates a new organization ruleset.
func createRuleset(ctx context.Context, client *github.Client, orgName string, ruleset *github.Ruleset, logger zerolog.Logger) error {
	if _, _, err := client.Organizations.CreateOrganizationRuleset(ctx, orgName, ruleset); err != nil {
		return errors.Wrap(err, "Failed to deploy repository ruleset")
	}
	logger.Info().Msgf("Successfully created the %s ruleset for organization %s.", ruleset.Name, orgName)
	return nil
}

// editRuleset updates an existing organization ruleset.
func editRuleset(ctx context.Context, client *github.Client, orgName string, rulesetID int64, ruleset *github.Ruleset, logger zerolog.Logger) error {
	if _, _, err := client.Organizations.UpdateOrganizationRuleset(ctx, orgName, rulesetID, ruleset); err != nil {
		return errors.Wrap(err, "Failed to update repository ruleset")
	}
	logger.Info().Msgf("Successfully updated the %s ruleset for organization %s to match the configuration file.", ruleset.Name, orgName)
	return nil
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

// getTeamByName returns the team by its name.
func getTeamByName(ctx context.Context, client *github.Client, orgName, teamName string) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamBySlug(ctx, orgName, teamName)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get team")
	}
	return team, nil
}

// getTeamByID returns the team by its ID.
func getTeamByID(ctx context.Context, client *github.Client, orgID, teamID int64) (*github.Team, error) {
	team, _, err := client.Teams.GetTeamByID(ctx, orgID, teamID)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get team")
	}
	return team, nil
}

// getOrgID returns the organization ID for a given organization name.
func getOrgID(ctx context.Context, client *github.Client, orgName string) (int64, error) {
	org, _, err := client.Organizations.Get(ctx, orgName)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to get organization")
	}
	return org.GetID(), nil
}

// getCustomRepoRolesForOrg returns the custom repository roles for an organization.
func getCustomRepoRolesForOrg(ctx context.Context, client *github.Client, orgName string) (*github.OrganizationCustomRepoRoles, error) {
	customRepoRoles, _, err := client.Organizations.ListCustomRepoRoles(ctx, orgName)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get custom repository roles for organization: %s", orgName)
	}
	return customRepoRoles, nil
}

// getAuthenticatedApp returns the authenticated app.
func getAuthenticatedApp(ctx context.Context, client *github.Client) (*github.App, error) {
	app, _, err := client.Apps.Get(ctx, "")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get app")
	}
	return app, nil
}

// getInstallationsForAuthenticatedApp returns the installations for the authenticated app.
func getInstallationsForAuthenticatedApp(ctx context.Context, client *github.Client) ([]*github.Installation, error) {
	installations, _, err := client.Apps.ListInstallations(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to list installations")
	}
	return installations, nil
}

// newJWTClient creates a new client using a JSON Web Token (JWT) for authentication.
func newJWTClient() (*github.Client, error) {
	config, err := ReadConfig("config.yml")
	if err != nil {
		panic(err)
	}

	// Create a file of the private key
	_, err = os.Create("private-key.pem")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create private key file")
	}

	// Write the configured private key to the file
	err = os.WriteFile("private-key.pem", []byte(config.Github.App.PrivateKey), 0600)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to write private key to file")
	}

	// Create a new JWT client
	itr, err := ghinstallation.NewAppsTransportKeyFromFile(http.DefaultTransport, config.Github.App.IntegrationID, "private-key.pem")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create JWT client")
	}
	client := github.NewClient(&http.Client{Transport: itr})

	// Delete the private key file
	err = os.Remove("private-key.pem")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to delete private key file")
	}

	return client, nil
}

// getOrgInstallations returns a map of organization names and their corresponding installation IDs.
func getOrgInstallations(ctx context.Context, client *github.Client) (map[string]int64, error) {
	installations, err := getInstallationsForAuthenticatedApp(ctx, client)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get installations for authenticated app")
	}

	orgInstallations := make(map[string]int64)
	for _, installation := range installations {
		if installation.Account != nil && installation.Account.Login != nil {
			orgInstallations[*installation.Account.Login] = *installation.ID
		}
	}

	return orgInstallations, nil
}

// getOrgInstallationID returns the installation ID of the authenticated app for a given organization.
func getOrgAppInstallationID(ctx context.Context, client *github.Client, orgName string) (int64, error) {
	installation, _, err := client.Apps.FindOrganizationInstallation(ctx, orgName)
	if err != nil {
		return 0, errors.Wrap(err, "Failed to find organization installation")
	}

	return installation.GetID(), nil
}

// getOrgRulesets returns the rulesetID for a given organization and ruleset name.
func getOrgRulesets(ctx context.Context, client *github.Client, orgName string) ([]*github.Ruleset, error) {
	rulesets, _, err := client.Organizations.GetAllOrganizationRulesets(ctx, orgName)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get organization rulesets")
	}

	return rulesets, nil
}

// extractRepoFullName extracts the repository full name from a GitHub URL.
func getRepoFullNameFromURL(githubURL string) (string, error) {
	parsedURL, err := url.Parse(githubURL)
	if err != nil {
		return "", err
	}

	// The path will be in the format "/owner/repo"
	path := strings.Trim(parsedURL.Path, "/")
	return path, nil
}

// getRuleSetFiles returns a list of the ruleset files in the rulesets directory
func getRuleSetFiles() ([]string, error) {
	files, err := os.ReadDir("rulesets")
	if err != nil {
		return nil, errors.Wrap(err, "Failed to read rulesets directory")
	}

	var ruleSetFiles []string
	for _, file := range files {
		if !file.IsDir() {
			ruleSetFiles = append(ruleSetFiles, filepath.Join("rulesets", file.Name()))
		}
	}

	return ruleSetFiles, nil
}
