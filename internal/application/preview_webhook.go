package application

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode"

	"pier/internal/platform/apperr"
)

// pullRequestAction is what a pull request event asks of its preview.
type pullRequestAction int

const (
	pullRequestIgnored pullRequestAction = iota
	pullRequestOpened
	pullRequestUpdated
	pullRequestClosed
)

const (
	maxPullRequestNumber = 1_000_000_000
	maxBranchLength      = 255
	maxTitleLength       = 200
	maxAuthorLength      = 100
)

// pullRequestEvent is the part of a pull/merge request event previews use,
// the same for every provider.
type pullRequestEvent struct {
	Action     pullRequestAction
	Number     int
	Branch     string
	BaseBranch string
	CommitSHA  string
	Title      string
	Author     string
	// Fork is true unless the head is provably the base repository.
	Fork bool
}

// isPullRequestEvent recognizes pull/merge request events of each provider.
func isPullRequestEvent(provider GitProvider, header http.Header) bool {
	switch provider {
	case ProviderGitHub:
		return header.Get("X-GitHub-Event") == "pull_request"
	case ProviderGitea:
		event := header.Get("X-Gitea-Event")
		return event == "pull_request" || event == "pull_request_sync"
	case ProviderGitLab:
		return header.Get("X-Gitlab-Event") == "Merge Request Hook"
	case ProviderBitbucket:
		return isBitbucketPullRequest(header)
	}
	return false
}

// parsePullRequest reads and validates a pull request event. An action
// previews do not react to is returned as pullRequestIgnored, unvalidated.
func parsePullRequest(provider GitProvider, body []byte) (pullRequestEvent, error) {
	var event pullRequestEvent
	var err error
	if provider == ProviderGitLab {
		event, err = parseGitLabMergeRequest(body)
	} else {
		event, err = parseGitHubPullRequest(body)
	}
	if err != nil {
		return pullRequestEvent{}, apperr.Invalid("Evento de pull request inválido.")
	}
	if event.Action == pullRequestIgnored {
		return event, nil
	}
	return event, event.validate()
}

type pullRequestRepository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
}

type pullRequestSide struct {
	Ref  string                 `json:"ref"`
	SHA  string                 `json:"sha"`
	Repo *pullRequestRepository `json:"repo"`
}

// githubPullRequest is a pull_request event of GitHub; Gitea and Forgejo
// send the same shape.
type githubPullRequest struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head pullRequestSide `json:"head"`
		Base pullRequestSide `json:"base"`
	} `json:"pull_request"`
}

func parseGitHubPullRequest(body []byte) (pullRequestEvent, error) {
	var payload githubPullRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return pullRequestEvent{}, err
	}
	pull := payload.PullRequest
	number := payload.Number
	if number == 0 {
		number = pull.Number
	}
	return pullRequestEvent{
		Action: githubAction(payload.Action), Number: number,
		Branch: pull.Head.Ref, BaseBranch: pull.Base.Ref, CommitSHA: pull.Head.SHA,
		Title: pull.Title, Author: pull.User.Login, Fork: fromFork(pull.Head.Repo, pull.Base.Repo),
	}, nil
}

func githubAction(action string) pullRequestAction {
	switch action {
	case "opened", "reopened":
		return pullRequestOpened
	case "synchronize", "synchronized":
		return pullRequestUpdated
	case "closed":
		return pullRequestClosed
	}
	return pullRequestIgnored
}

// fromFork fails closed: a missing repository (a deleted fork) or any
// mismatch between head and base counts as a fork.
func fromFork(head, base *pullRequestRepository) bool {
	if head == nil || base == nil || head.FullName == "" {
		return true
	}
	if head.ID != 0 && base.ID != 0 && head.ID != base.ID {
		return true
	}
	return !strings.EqualFold(head.FullName, base.FullName)
}

// gitlabMergeRequest is a "Merge Request Hook" event of GitLab.
type gitlabMergeRequest struct {
	User struct {
		Username string `json:"username"`
	} `json:"user"`
	ObjectAttributes struct {
		IID             int    `json:"iid"`
		Title           string `json:"title"`
		Action          string `json:"action"`
		SourceBranch    string `json:"source_branch"`
		TargetBranch    string `json:"target_branch"`
		SourceProjectID int64  `json:"source_project_id"`
		TargetProjectID int64  `json:"target_project_id"`
		LastCommit      struct {
			ID string `json:"id"`
		} `json:"last_commit"`
	} `json:"object_attributes"`
}

func parseGitLabMergeRequest(body []byte) (pullRequestEvent, error) {
	var payload gitlabMergeRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return pullRequestEvent{}, err
	}
	request := payload.ObjectAttributes
	fork := request.SourceProjectID == 0 || request.SourceProjectID != request.TargetProjectID
	return pullRequestEvent{
		Action: gitlabAction(request.Action), Number: request.IID,
		Branch: request.SourceBranch, BaseBranch: request.TargetBranch, CommitSHA: request.LastCommit.ID,
		Title: request.Title, Author: payload.User.Username, Fork: fork,
	}, nil
}

func gitlabAction(action string) pullRequestAction {
	switch action {
	case "open", "reopen":
		return pullRequestOpened
	case "update":
		return pullRequestUpdated
	case "close", "merge":
		return pullRequestClosed
	}
	return pullRequestIgnored
}

// validate rejects what the preview cannot trust. The head commit is the
// only input that selects code, so it must be a full SHA; the branch, title
// and author are only displayed.
func (e *pullRequestEvent) validate() error {
	if e.Number < 1 || e.Number > maxPullRequestNumber {
		return apperr.Invalid("Número de pull request inválido.")
	}
	e.Branch = cleanDisplayText(e.Branch, maxBranchLength)
	e.BaseBranch = strings.TrimSpace(e.BaseBranch)
	e.Title = cleanDisplayText(e.Title, maxTitleLength)
	e.Author = cleanDisplayText(e.Author, maxAuthorLength)
	if e.Action == pullRequestClosed {
		return nil
	}
	e.CommitSHA = strings.ToLower(strings.TrimSpace(e.CommitSHA))
	if !commitPattern.MatchString(e.CommitSHA) {
		return apperr.Invalid("O evento não informa o commit do pull request.")
	}
	if e.Branch == "" {
		return apperr.Invalid("O evento não informa a branch do pull request.")
	}
	return nil
}

// cleanDisplayText drops control and format characters (bidirectional
// overrides could disguise a title) and bounds the length in runes.
func cleanDisplayText(text string, limit int) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.TrimSpace(text))
	if runes := []rune(cleaned); len(runes) > limit {
		cleaned = string(runes[:limit])
	}
	return cleaned
}
