package application

import (
	"context"
	"encoding/json"
	"net/http"

	"pier/internal/gitprovider"
	"pier/internal/platform/apperr"
)

// CommitResolver expands the abbreviated commit Bitbucket sends in pull
// request events (previews pin a full SHA).
type CommitResolver interface {
	ResolveBitbucketCommit(ctx context.Context, kind, id, repository, commit string) (string, error)
}

// SetCommitResolver installs who expands Bitbucket commits.
func (s *Service) SetCommitResolver(resolver CommitResolver) { s.commits = resolver }

type bitbucketRef struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Target struct {
		Hash string `json:"hash"`
	} `json:"target"`
}

// parseBitbucketPush reads the first change of a Bitbucket "repo:push"
// event as a push of the other providers. A body that is not one yields an
// empty event (no ref), like before.
func parseBitbucketPush(body []byte) (pushEvent, bool) {
	var payload struct {
		Push struct {
			Changes []struct {
				New    *bitbucketRef `json:"new"`
				Old    *bitbucketRef `json:"old"`
				Closed bool          `json:"closed"`
			} `json:"changes"`
		} `json:"push"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Push.Changes) == 0 {
		return pushEvent{}, true
	}
	change := payload.Push.Changes[0]
	ref, deleted := change.New, change.New == nil || change.Closed
	if ref == nil {
		ref = change.Old
	}
	if ref == nil {
		return pushEvent{}, true
	}
	prefix := "refs/heads/"
	if ref.Type == "tag" {
		prefix = "refs/tags/"
	}
	event := pushEvent{Ref: prefix + ref.Name, Deleted: deleted}
	if !deleted {
		event.After = ref.Target.Hash
	}
	return event, true
}

func isBitbucketPullRequest(header http.Header) bool {
	switch header.Get("X-Event-Key") {
	case "pullrequest:created", "pullrequest:updated", "pullrequest:fulfilled", "pullrequest:rejected":
		return true
	}
	return false
}

type bitbucketSide struct {
	Branch struct {
		Name string `json:"name"`
	} `json:"branch"`
	Commit struct {
		Hash string `json:"hash"`
	} `json:"commit"`
	Repository struct {
		FullName string `json:"full_name"`
		UUID     string `json:"uuid"`
	} `json:"repository"`
}

type bitbucketPullRequest struct {
	PullRequest struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Author struct {
			Nickname    string `json:"nickname"`
			DisplayName string `json:"display_name"`
		} `json:"author"`
		Source      bitbucketSide `json:"source"`
		Destination bitbucketSide `json:"destination"`
	} `json:"pullrequest"`
}

// normalizePullRequest rewrites a Bitbucket pull request event into the
// GitHub shape the previews read, with the full head commit. Other
// providers pass through unchanged.
func (s *Service) normalizePullRequest(ctx context.Context, app Application, provider GitProvider, body []byte) ([]byte, error) {
	if provider != ProviderBitbucket {
		return body, nil
	}
	var payload bitbucketPullRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, apperr.Invalid("Evento de pull request inválido.")
	}
	pull := payload.PullRequest
	action, commit := "synchronize", pull.Source.Commit.Hash
	if pull.State != "OPEN" {
		action, commit = "closed", ""
	}
	if action != "closed" && !ValidCommit(commit) {
		if s.commits == nil {
			return nil, apperr.Invalid("O evento não informa o commit do pull request.")
		}
		full, err := s.commits.ResolveBitbucketCommit(ctx, gitprovider.KindApplication, app.ID, pull.Source.Repository.FullName, commit)
		if err != nil {
			return nil, apperr.Invalid("Não foi possível obter o commit completo do pull request no Bitbucket. " +
				"Para repositórios privados, salve um token do Bitbucket na integração Git da aplicação.")
		}
		commit = full
	}
	author := pull.Author.Nickname
	if author == "" {
		author = pull.Author.DisplayName
	}
	headRepository := pull.Source.Repository.FullName
	if pull.Source.Repository.UUID != pull.Destination.Repository.UUID {
		headRepository = "fork:" + headRepository
	}
	return json.Marshal(githubShapedPullRequest(action, pull.ID, pull.Title, author,
		pullRequestSide{Ref: pull.Source.Branch.Name, SHA: commit, Repo: &pullRequestRepository{FullName: headRepository}},
		pullRequestSide{Ref: pull.Destination.Branch.Name, Repo: &pullRequestRepository{FullName: pull.Destination.Repository.FullName}}))
}

func githubShapedPullRequest(action string, number int, title, author string, head, base pullRequestSide) githubPullRequest {
	var event githubPullRequest
	event.Action, event.Number = action, number
	event.PullRequest.Number, event.PullRequest.Title = number, title
	event.PullRequest.User.Login = author
	event.PullRequest.Head, event.PullRequest.Base = head, base
	return event
}
