package main

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"

	"github.com/PuerkitoBio/goquery"
	"github.com/yuin/goldmark"
	goldmarkHtml "github.com/yuin/goldmark/renderer/html"
)

var (
	IMAGE_IGNORE_LIST = []string{
		"badge.svg",
		"img.shields.io",
		"/badges/",
		"/badge/",
		"/sponsors/",
		"producthunt.com/widgets",
		"status.png",
		"status.svg",
		"circleci.com",
		"awesome.re",
	}

	GITHUB_REGEXP = regexp.MustCompile(
		`(?i)github\.com\/(.+?)\/(.+?)(?:[\s/?]|$)`,
	)
)

type GithubRepo struct {
	User string
	Repo string
}

func getGitHubReadme(repo GithubRepo) ([]byte, *url.URL, error) {
	readmeUrl, err := url.Parse(
		fmt.Sprintf(
			"https://raw.githubusercontent.com/%s/%s/refs/heads/main/README.md",
			repo.User, repo.Repo,
		),
	)
	if err != nil {
		return []byte{}, &url.URL{}, err
	}

	res, err := http.Get(readmeUrl.String())
	if err != nil {
		return []byte{}, &url.URL{}, err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return []byte{}, &url.URL{}, err
	}

	return data, readmeUrl, nil
}

func containsNeedles(needles []string, v string) bool {
	for _, needle := range needles {
		if strings.Contains(v, needle) {
			return true
		}
	}
	return false
}

func getNodeAttr(node *html.Node, key string) (html.Attribute, bool) {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr, true
		}
	}
	return html.Attribute{}, false
}

func downloadImage(repo GithubRepo, imageUrl *url.URL, wg *sync.WaitGroup) error {
	wg.Add(1)
	defer wg.Done()

	res, err := http.Get(imageUrl.String())
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	sum := crc32.ChecksumIEEE(data)
	hex := strconv.FormatUint(uint64(sum), 16)

	ext := filepath.Ext(imageUrl.Path)

	filename := fmt.Sprintf("%s_%s_%s%s", repo.User, repo.Repo, hex, ext)

	return os.WriteFile("./downloads/"+filename, data, 0644)
}

func doRepoReadme(repo GithubRepo, wg *sync.WaitGroup) ([]GithubRepo, error) {
	fmt.Printf("scraping %s/%s\n", repo.User, repo.Repo)

	data, readmeUrl, err := getGitHubReadme(repo)
	if err != nil {
		return []GithubRepo{}, nil
	}

	var buf bytes.Buffer

	err = goldmark.New(
		goldmark.WithRendererOptions(
			goldmarkHtml.WithUnsafe(),
		),
	).Convert(data, &buf)

	if err != nil {
		return []GithubRepo{}, nil
	}

	// os.WriteFile("test.html", buf.Bytes(), 0644)

	doc, err := goquery.NewDocumentFromReader(&buf)
	if err != nil {
		return []GithubRepo{}, nil
	}

	for _, node := range doc.Find("img").Nodes {
		attr, exists := getNodeAttr(node, "src")
		if !exists {
			continue
		}

		imageUrl, err := url.Parse(strings.TrimSpace(attr.Val))
		if err != nil {
			continue
		}

		if !imageUrl.IsAbs() {
			imageUrl = readmeUrl.ResolveReference(imageUrl)
		}

		if containsNeedles(IMAGE_IGNORE_LIST, imageUrl.String()) {
			break
		}

		go downloadImage(repo, imageUrl, wg)

		break
	}

	var githubRepos []GithubRepo

	for _, node := range doc.Find("a").Nodes {
		attr, exists := getNodeAttr(node, "href")
		if !exists {
			continue
		}

		url := strings.TrimSpace(attr.Val)

		matches := GITHUB_REGEXP.FindStringSubmatch(url)
		if len(matches) > 0 {
			githubRepos = append(githubRepos, GithubRepo{
				User: matches[1],
				Repo: matches[2],
			})
		}
	}

	return githubRepos, nil
}

func main() {
	var wg sync.WaitGroup

	awesomeRepo := GithubRepo{User: "avelino", Repo: "awesome-go"}

	repos, err := doRepoReadme(awesomeRepo, &wg)
	// _, err := doRepoReadme(GithubRepo{User: "leozz37", Repo: "hare"})

	if err != nil {
		panic(err)
	}

	for _, repo := range repos {
		_, err := doRepoReadme(repo, &wg)
		if err != nil {
			fmt.Println(err)
		}
	}

	wg.Wait()
}
