package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/bradleyfalzon/ghinstallation/v2"
)

func main() {
	var GitHubAppID = os.Getenv("MK_REPO_APP_ID")
	var GitHubAppInstallID = os.Getenv("MK_REPO_APP_INSTALL_ID")
	var GitHubAppPrivateKey = os.Getenv("MK_REPO_APP_PRIVATE_KEY")
	token, err := getToken(GitHubAppID, GitHubAppInstallID, GitHubAppPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	print(*token)
}

func getToken(app_id, app_install_id, app_private_key string) (*string, error) {
	id, err := strconv.ParseInt(app_id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse app id: %w", err)
	}
	installID, err := strconv.ParseInt(app_install_id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse app install id: %w", err)
	}
	itr, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, id, installID, app_private_key)
	if err != nil {
		return nil, fmt.Errorf("init ghinstallation: %w", err)
	}
	token, err := itr.Token(context.Background())
	return &token, err
}
