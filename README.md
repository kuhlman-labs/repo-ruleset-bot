# Repo Ruleset Bot

## Purpose
The **Repo Ruleset Bot** is a GitHub App designed to manage repository rulesets for organizations. It listens to specific GitHub events and ensures that the repository rulesets are consistent with the predefined ruleset configuration. The bot can handle events such as the creation, editing, and deletion of repository rulesets, and it can also manage custom repository roles and teams.

## How to Create the GitHub App in GitHub

1. **Navigate to GitHub Settings**:
   - Go to your GitHub User account or Organization settings.
   - Select "Developer settings" from the sidebar.
   - Click on "GitHub Apps".

2. **Create a New GitHub App**:
   - Click on "New GitHub App".
   - Fill in the required details:
     - **GitHub App name**: Choose a unique name for your app.
     - **Homepage URL**: Provide a URL for your app's homepage.
     - **Webhook URL**: Set this to the URL where your app will receive webhook events (e.g., `http://your-server.com/api/github/hook`).
     - **Webhook secret**: Generate a secret for securing webhook payloads.
   - **Permissions**:
     - Under "Organization permissions":
       - **Administration** -> **Read & Write**. This is needed to manage organization repository rulesets.
       - **Members** -> **Read-only**. This is needed to make calls to the Teams API.
       - **Custom repository roles** -> **Read-only**. This is needed to make calls to the Custom Repository Roles API.
   - **Subscribe to Events**:
     - Subscribe to the "Repository ruleset" event.
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
6. Add the ruleset to your repository and configure the path to the ruleset in the `config.yml` file.

**Important Note**: Any Teams, Custom Repository Roles, or Apps that are included as bypass actors in the ruleset must be added to the `config.yml` file and exist in the Organization that the ruleset is going to be applied to.

## How to Configure the [`config.yml`](config.yml) File

Create a [`config.yml`](config.yml) file in the root directory of your project with the following structure:

```yaml
server:
  address: "127.0.0.1"
  port: 8080

ruleset: "path/to/your/ruleset.json"

custom_repo_roles:
  - "role1"
  - "role2"
teams:
  - "team1"
  - "team2"

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
- **ruleset**: The path to the JSON file containing the ruleset configuration.
- **custom_repo_roles**: A list of custom repository roles to add as bypass actors to the ruleset.
- **teams**: A list of teams to be added as bypass actors to the ruleset.
- **github**:
  - **app**:
    - `v3_api_url`: The URL for the GitHub v3 API.
    - `integration_id`: The GitHub App's integration ID.
    - `private_key`: The private key for the GitHub App.
    - `webhook_secret`: The secret for verifying webhook payloads.

## How to Run the App

1. **Install Dependencies**:
   - Ensure you have Go installed on your machine.
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

   - The server will start and listen for GitHub events on the specified address and port. The default path it will listen on is `/api/github/hook`.

## Example Usage

Once the server is running, it will listen for the configured GitHub events and manage the repository rulesets according to the rules defined in the ruleset file you have specified in the config. The bot will log its actions and any errors encountered during processing.

## Contributing

Feel free to open issues or submit pull requests if you find any bugs or have suggestions for improvements.

## License

This project is licensed under the MIT License. See the [`LICENSE`](LICENSE) file for details.