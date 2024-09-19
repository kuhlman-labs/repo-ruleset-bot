# Repo Ruleset Bot

## Overview
The **Repo Ruleset Bot** is a GitHub App designed to manage repository rulesets for organizations. It listens to specific GitHub events and ensures that the repository rulesets are consistent with the predefined ruleset configuration. The bot will enforce the ruleset configuration by reverting any changes made to the ruleset that aren't sent by the app, ensuring that the ruleset is always in the desired state.

## Creating the GitHub App

1. **Navigate to GitHub Settings**:
   - Go to your GitHub User account or Organization settings.
   - Select "Developer settings" from the sidebar.
   - Click on "GitHub Apps".

2. **Create a New GitHub App**:
   - Click on "New GitHub App".
   - Fill in the required details:
     - **GitHub App name**: Choose a unique name for your app.
     - **Homepage URL**: Provide the URL for the GitHub repository that is hosting the GitHub App code. **NOTE**: This must be a repository in a GitHub organization where the app will be installed. (e.g., https://github.com/kuhlman-labs/repo-ruleset-bot) 
     - **Webhook URL**: Set this to the URL where your app will receive webhook events (e.g., `http://your-server.com/api/github/hook`).
     - **Webhook secret**: Generate a secret for securing webhook payloads.
   - **Permissions**:
     - Under "Organization permissions":
       - **Administration** -> **Read & Write**. This is needed to manage organization repository rulesets.
       - **Members** -> **Read-only**. This is needed to make calls to the Teams API.
       - **Custom repository roles** -> **Read-only**. This is needed to make calls to the Custom Repository Roles API.
     - Under "Repository permissions":
       - **Contents** -> **Read-only**. This is needed to read the release assets to get the ruleset configuration.
   - **Subscribe to Events**:
     - Subscribe to the "Repository ruleset" event.
     - Subscribe to the "Release" event.
   - **Save**: Click "Create GitHub App".

3. **Generate Private Key**:
   - After creating the app, generate a private key and download it. This key will be used to authenticate your app.

4. **Install the GitHub App**:
   - Install the app on the desired organizations.

## How to Create the Ruleset Configuration File

1. Log In to GitHub in an Organization that you are an Admin of.
2. In the settings navigate to the Repository section and click on the "Rulesets" tab.
3. Click on "New ruleset"
4. Compose the ruleset as you see fit.
5. Once you have saved the ruleset, you can download the JSON representation of the ruleset. Click on the open addional options menu and select "Export Ruleset".
6. Add the ruleset to the `rulesets` directory.

**Important Note**: Any Teams, Custom Repository Roles, or Apps that are included as bypass actors in the ruleset must exist in the Organization that the ruleset is going to be applied to.

## How to Configure the [`config.yml`](config.yml) File

Create a [`config.yml`](config.yml) file in the root directory of your project with the following structure:

```yaml
server:
  address: "127.0.0.1"
  port: 8080

github:
  v3_api_url: "https://api.github.com"
  app:
    integration_id: YOUR_APP_ID
    webhook_secret: YOUR_WEBHOOK_SECRET
    private_key: |
      -----BEGIN RSA PRIVATE KEY-----
      YOUR_PRIVATE_KEY
      -----END RSA PRIVATE KEY-----
```

### Configuration Fields

- **server**:
  - `address`: The address where the server will run.
  - `port`: The port on which the server will listen.
- **github**:
  - **app**:
    - `v3_api_url`: The URL for the GitHub v3 API.
    - `integration_id`: The GitHub App's integration ID.
    - `private_key`: The private key for the GitHub App.
    - `webhook_secret`: The secret for verifying webhook payloads.

## How to Run the App

1. **Clone the Repository**:
   - Clone or Fork the Repository to a GitHub Organization where you have Admin access.
   - Clone the repository to your local machine or the machine which will host the app:

1. **Install Dependencies**:
   - Ensure you have Go installed on the machine.
   - Install the required Go packages:
     ```sh
     go mod tidy
     ```

2. **Build the Application**:
   - Build the Go application:
     ```sh
     go build -o repo-ruleset-bot
     ```

3. **Run the App**:
   - Run the server with the configuration file:
     ```sh
     ./repo-ruleset-bot
     ```

   - The server will start and listen for GitHub events on the specified address and port. ***The default path it will listen on is `/api/github/hook`.***

## Features

Once the App is set up and running, it will listen for the ruleset events and deploy the rulesets located in the `rulesets` directory when the app gets installed to an Organization. If someone modifies or deletes the ruleset from the GitHub UI, the app will revert the changes to the ruleset.

- **Deploy Ruleset**:
  - When the app is installed to an Organization, it will deploy the rulesets located in the `rulesets` directory of this repository to the Organization.
  - If the Ruleset gets deleted, the app will redeploy the ruleset to the Organization.
- **Revert Changes**:
  - If a user modifies the ruleset the app will revert the changes.
- **Updating the Ruleset**:
  - To update to a new version of the ruleset, you can update the JSON file and create a new release in the repository. This will trigger an update to the ruleset in the Organizations where the app is installed.

## Contributing

Feel free to open issues or submit pull requests if you find any bugs or have suggestions for improvements.

## License

This project is licensed under the MIT License. See the [`LICENSE`](LICENSE) file for details.