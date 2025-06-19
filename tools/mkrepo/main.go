package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v63/github"
	"gopkg.in/yaml.v3"
)

type Repo struct {
	Repo        string
	Info        string
	Developer   string
	DeveloperID string `yaml:"developer_id"`
}

func main() {
	var runFlag bool
	var checkFlag string
	flag.StringVar(&checkFlag, "check", "", "check yaml file")
	flag.BoolVar(&runFlag, "run", false, "start create repo")
	flag.Parse()
	if len(checkFlag) > 0 {
		err := check(checkFlag)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if runFlag {
		err := run()
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	flag.PrintDefaults()
}

func check(filename string) error {
	var GitHubOrg = os.Getenv("MK_REPO_ORG")
	var GitHubAppID = os.Getenv("MK_REPO_APP_ID")
	var GitHubAppInstallID = os.Getenv("MK_REPO_APP_INSTALL_ID")
	var GitHubAppPrivateKey = os.Getenv("MK_REPO_APP_PRIVATE_KEY")
	// 初始化github client
	client, err := initGithubClient(GitHubAppID, GitHubAppInstallID, GitHubAppPrivateKey)
	if err != nil {
		return fmt.Errorf("init github client: %w", err)
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	result := struct {
		Repos []*Repo
	}{}
	err = yaml.Unmarshal(data, &result)
	if err != nil {
		return fmt.Errorf("unmarshal repo config: %w", err)
	}
	// 检查仓库是否已存在
	var existsRepo []string
	developerMap := make(map[string]string)
	for i := range result.Repos {
		repo := result.Repos[i]
		if len(repo.Repo) == 0 {
			log.Fatal("missing repo field")
		}
		if len(repo.Developer) == 0 {
			log.Fatal("missing developer field")
		}
		// 对开发者去除
		developerMap[repo.Developer] = repo.Repo
		_, _, err := client.Repositories.Get(context.Background(), GitHubOrg, repo.Repo)
		if err == nil {
			log.Println("check repo", repo, "exists")
			existsRepo = append(existsRepo, result.Repos[i].Repo)
			continue
		}
		log.Println("check", repo, "not exists")
	}
	if len(existsRepo) > 0 {
		return fmt.Errorf("repos %s exists", strings.Join(existsRepo, ","))
	}
	// 检查开发者是否不存在
	var notFoundDeveloper []string
	for developer := range developerMap {
		_, err = getDeveloperID(client, developer)
		if err != nil {
			log.Println("check developer", developer, "not found")
			notFoundDeveloper = append(notFoundDeveloper, developer)
			continue
		}
		log.Println("check", developer, "found")
	}
	if len(notFoundDeveloper) > 0 {
		return fmt.Errorf("developer %s not found", strings.Join(existsRepo, ","))
	}
	return nil
}

func run() error {
	var GitHubOrg = os.Getenv("MK_REPO_ORG")
	var GitHubManagerRepo = os.Getenv("MK_REPO_MANAGER_REPO")
	var GitHubAppID = os.Getenv("MK_REPO_APP_ID")
	var GitHubAppInstallID = os.Getenv("MK_REPO_APP_INSTALL_ID")
	var GitHubAppPrivateKey = os.Getenv("MK_REPO_APP_PRIVATE_KEY")
	var GitHubWebhookUrl = os.Getenv("MK_REPO_WEBHOOK_URL")
	var GitHubWebhookSecret = os.Getenv("MK_REPO_WEBHOOK_SECRET")
	// 初始化github client
	client, err := initGithubClient(GitHubAppID, GitHubAppInstallID, GitHubAppPrivateKey)
	if err != nil {
		return fmt.Errorf("init github client: %w", err)
	}
	// 从远程读取repos.yaml文件和repos_history.yaml
	repos, sha, err := getRepos(client, GitHubOrg, GitHubManagerRepo)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}
	history, historySha, err := getHistory(client, GitHubOrg, GitHubManagerRepo)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}
	// repos.yaml的记录被视为新增仓库
	// 根据developer字段补充developer_id
	newRepo := []string{}
	developerMap := map[string]string{}
	for i := range repos {
		repo := repos[i]
		if len(repo.DeveloperID) > 0 {
			continue
		}
		if len(developerMap[repo.Developer]) > 0 {
			repo.DeveloperID = developerMap[repo.Developer]
		} else {
			repo.DeveloperID, err = getDeveloperID(client, repo.Developer)
			if err != nil {
				return fmt.Errorf("get developer id: %w", err)
			}
			developerMap[repo.Developer] = repo.DeveloperID
			time.Sleep(time.Second)
		}
		history = append(history, repo)
		newRepo = append(newRepo, repo.Repo)
	}
	if len(newRepo) == 0 {
		return nil
	}

	// 批量创建新增记录的应用仓库
	var createdRepo []string
	for _, repo := range newRepo {
		log.Println("create", repo)
		err = createRepo(client, GitHubOrg, repo, GitHubWebhookUrl, GitHubWebhookSecret)
		if err != nil {
			// 如果批量创建发生错误，删除已经创建的仓库
			for i := range createdRepo {
				_, err := client.Repositories.Delete(context.Background(), GitHubOrg, createdRepo[i])
				if err != nil {
					log.Println("delete repo: %w", err)
				}
				time.Sleep(time.Second)
			}
			return fmt.Errorf("create %s: %w", repo, err)
		}
		createdRepo = append(createdRepo, repo)
		time.Sleep(time.Second)
	}
	// 将repos.yaml文件恢复成默认模板，避免多个PR合并冲突
	data, _, err := getContent(
		client,
		GitHubOrg, GitHubManagerRepo,
		"repos.yaml",
		"84c8b799373fadcedc93add6cf1d61081a95d259",
	)
	if err != nil {
		return fmt.Errorf("get repos template: %w", err)
	}
	opts := &github.RepositoryContentFileOptions{
		Message: github.String("chore: Restore repos.yaml"),
		Content: data,
		SHA:     github.String(sha),
	}
	_, _, err = client.Repositories.UpdateFile(
		context.Background(),
		GitHubOrg, GitHubManagerRepo,
		"repos.yaml", opts,
	)
	if err != nil {
		return fmt.Errorf("restore repos: %w", err)
	}
	// 保存新数据到repo_history.yaml文件
	data, err = marshalYAML(struct{ Repos []*Repo }{history})
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	opts = &github.RepositoryContentFileOptions{
		Message: github.String("chore: Update history/repos_history.yaml"),
		Content: data,
		SHA:     github.String(historySha),
	}
	_, _, err = client.Repositories.UpdateFile(
		context.Background(),
		GitHubOrg, GitHubManagerRepo,
		"history/repos_history.yaml", opts,
	)
	if err != nil {
		return fmt.Errorf("save history: %w", err)
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

func getContent(client *github.Client, org, repo, path, ref string) (data []byte, sha string, err error) {
	fileContent, _, _, err := client.Repositories.GetContents(
		context.Background(),
		org, repo, path,
		&github.RepositoryContentGetOptions{Ref: ref},
	)
	if err != nil {
		return nil, "", fmt.Errorf("read repo config: %w", err)
	}
	content, err := fileContent.GetContent()
	if err != nil {
		return nil, "", fmt.Errorf("read repo config: %w", err)
	}
	return []byte(content), fileContent.GetSHA(), nil
}

func getRepos(client *github.Client, org, repo string) (data []*Repo, sha string, err error) {
	content, sha, err := getContent(client, org, repo, "repos.yaml", "")
	if err != nil {
		return nil, "", fmt.Errorf("get repo content: %w", err)
	}
	result := struct {
		Repos []*Repo
	}{}
	err = yaml.Unmarshal(content, &result)
	if err != nil {
		return nil, "", fmt.Errorf("unmarshal repo config: %w", err)
	}
	return result.Repos, sha, nil
}

func getHistory(client *github.Client, org, repo string) (data []*Repo, sha string, err error) {
	content, sha, err := getContent(client, org, repo, "history/repos_history.yaml", "")
	if err != nil {
		return nil, "", fmt.Errorf("get repo content: %w", err)
	}
	result := struct {
		Repos []*Repo
	}{}
	err = yaml.Unmarshal(content, &result)
	if err != nil {
		return nil, "", fmt.Errorf("unmarshal repo config: %w", err)
	}
	return result.Repos, sha, nil
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

func setCustomProperties(client *github.Client, github_org, github_repo, key, val string) error {
	values := []*github.CustomPropertyValue{}
	values = append(values, &github.CustomPropertyValue{PropertyName: key, Value: github.String(val)})
	_, err := client.Repositories.CreateOrUpdateCustomProperties(
		context.Background(),
		github_org, github_repo,
		values,
	)
	return err
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
	// add custom properties
	err = setCustomProperties(client, github_org, github_repo, "kind", "app")
	if err != nil {
		return fmt.Errorf("set custom properties: %w", err)
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
	// Wait for the readme to be created
	// time.Sleep(time.Second)
	// _, _, err = client.Repositories.CreateHook(ctx, github_org, github_repo, &github.Hook{
	// 	Name: github.String("web"),
	// 	Config: &github.HookConfig{
	// 		URL:         github.String(webhook_url),
	// 		ContentType: github.String("json"),
	// 		Secret:      github.String(webhook_secret),
	// 	},
	// 	Events: []string{"push", "pull_request"},
	// })
	// if err != nil {
	// 	return fmt.Errorf("create hook: %w", err)
	// }
	return nil
}
