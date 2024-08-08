package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/github"
	"gopkg.in/yaml.v3"
)

type Repo struct {
	Repo        string
	Info        string
	Developer   string
	DeveloperID string `yaml:"developer_id"`
}

func main() {
	err := run()
	if err != nil {
		log.Panic(err)
	}
}

func run() error {
	var GitHubOrg = os.Getenv("MK_REPO_ORG")
	var GitHubManagerRepo = os.Getenv("MK_REPO_MANAGER_REPO")
	var GitHubAppID = os.Getenv("MK_REPO_APP_ID")
	var GitHubAppInstallID = os.Getenv("MK_REPO_APP_INSTALL_ID")
	var GitHubAppPrivateKey = os.Getenv("MK_REPO_APP_PRIVATE_KEY")
	var GitHubWebhookUrl = os.Getenv("MK_REPO_WEBHOOK_URL")
	var GitHubWebhookSecret = os.Getenv("MK_REPO_WEBHOOK_SECRET")

	client, err := initGithubClient(GitHubAppID, GitHubAppInstallID, GitHubAppPrivateKey)
	if err != nil {
		return fmt.Errorf("init github client: %w", err)
	}
	var data []byte
	var oldDataSha string
	if len(os.Args) > 1 {
		data, err = os.ReadFile(os.Args[1])
		if err != nil {
			return fmt.Errorf("read repo config: %w", err)
		}

	} else {
		fileContent, _, _, err := client.Repositories.GetContents(context.Background(), GitHubOrg, GitHubManagerRepo, "repos.yaml", &github.RepositoryContentGetOptions{})
		if err != nil {
			return fmt.Errorf("read repo config: %w", err)
		}
		content, err := fileContent.GetContent()
		if err != nil {
			return fmt.Errorf("read repo config: %w", err)
		}
		data = []byte(content)
		oldDataSha = fileContent.GetSHA()
	}
	result := struct {
		Repos []*Repo
		Tip   string
	}{}
	err = yaml.Unmarshal(data, &result)
	if err != nil {
		return fmt.Errorf("unmarshal repo config: %w", err)
	}
	for i := range result.Repos {
		repo := result.Repos[i]
		if len(repo.DeveloperID) > 0 {
			continue
		}
		repo.DeveloperID, err = getDeveloperID(client, repo.Developer)
		if err != nil {
			return fmt.Errorf("get developer id: %w", err)
		}
		err = createRepo(client, GitHubOrg, repo.Repo, GitHubWebhookUrl, GitHubWebhookSecret)
		if err != nil {
			return fmt.Errorf("create repo: %w", err)
		}

		data, err = marshalYAML(result)
		if err != nil {
			return fmt.Errorf("marshal repo config: %w", err)
		}
		if len(oldDataSha) > 0 {
			opts := &github.RepositoryContentFileOptions{
				Message: github.String("chore: update developer_id"),
				Content: data,
				SHA:     github.String(oldDataSha),
			}
			_, _, err = client.Repositories.UpdateFile(context.Background(), GitHubOrg, GitHubManagerRepo, "repos.yaml", opts)
			if err != nil {
				return fmt.Errorf("create readme: %w", err)
			}
		} else {
			err = os.WriteFile(os.Args[1], data, 0644)
			if err != nil {
				return fmt.Errorf("create readme: %w", err)
			}
		}
		return nil
	}
	return nil
}

func marshalYAML(v interface{}) ([]byte, error) {
	var buff bytes.Buffer
	encoder := yaml.NewEncoder(&buff)
	encoder.SetIndent(2)
	err := encoder.Encode(v)
	return buff.Bytes(), err
}

func initGithubClient(app_id, app_install_id, app_private_key string) (*github.Client, error) {
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
	return github.NewClient(&http.Client{Transport: itr}), nil
}

func getDeveloperID(client *github.Client, username string) (string, error) {
	user, _, err := client.Users.Get(context.Background(), username)
	if err != nil {
		return "", fmt.Errorf("get user info: %w", err)
	}
	return fmt.Sprintf("%d", user.GetID()), nil
}

func createRepo(client *github.Client, github_org, github_repo, webhook_url, webhook_secret string) error {
	ctx := context.Background()

	_, _, err := client.Repositories.Create(ctx, github_org, &github.Repository{
		Name:    github.String(github_repo),
		Private: github.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	// 创建README文件
	readmeContent := "# " + github_repo
	opts := &github.RepositoryContentFileOptions{
		Message: github.String("Initial commit with README"),
		Content: []byte(readmeContent),
	}
	_, _, err = client.Repositories.CreateFile(ctx, github_org, github_repo, "README.md", opts)
	if err != nil {
		return fmt.Errorf("create readme: %w", err)
	}
	// wait create file finish
	time.Sleep(time.Second)
	_, _, err = client.Repositories.CreateHook(ctx, github_org, github_repo, &github.Hook{
		Name:   github.String("web"),
		Config: map[string]interface{}{"url": webhook_url, "content_type": "json", "secret": webhook_secret},
		Events: []string{"push", "pull_request"},
	})
	if err != nil {
		return fmt.Errorf("create hook: %w", err)
	}
	return nil
}
