package db

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skia-dev/glog"
	"go.skia.org/infra/go/eventbus"
	"go.skia.org/infra/go/gitinfo"
	"go.skia.org/infra/go/rietveld"
	"go.skia.org/infra/go/tiling"
	"go.skia.org/infra/go/util"
	"go.skia.org/infra/go/vcsinfo"
)

const (
	MAX_CACHE_SIZE = 1000
)

// CommitIDLong contains more detailed information about each commit,
// regardless of whether it came from an actual commit or a trybot.
type CommitIDLong struct {
	*CommitID
	Author string `json:"author"`
	Desc   string `json:"desc"`
}

// BranchTileBuilder is a high level interface to build tiles base on a datasource that
// originated via a version control system or from a code review system via trybot
// runs.
type BranchTileBuilder interface {
	// ListLong returns a slice of CommitIDLongs that appear in the given time
	// range from begin to end, and may be filtered by the 'source' parameter. If
	// 'source' is the empty string then no filtering is done.
	ListLong(begin, end time.Time, source string) ([]*CommitIDLong, error)

	// TileFromCommits returns a tile with given commits.
	TileFromCommits(commitIDs []*CommitIDLong) (*tiling.Tile, error)
}

type tileBuilder struct {
	db        DB
	vcs       vcsinfo.VCS
	review    *rietveld.Rietveld
	reviewURL string
	// cache is a cache for rietveld.Issue's. Note that gitinfo has its own cache
	// for Details(), so we don't need to cache the results.
	cache map[string]*rietveld.Issue
}

// NewBranchTileBuilder returns an instance of BranchTileBuilder that allows to create
// tiles based on the given VCS or code review system based on querying db.
// TODO(stephana): The EventBus is used to update the internal cache as commits are updated.
func NewBranchTileBuilder(db DB, git *gitinfo.GitInfo, reviewURL string, evt *eventbus.EventBus) BranchTileBuilder {
	review := rietveld.New(reviewURL, util.NewTimeoutClient())
	return &tileBuilder{
		db:        db,
		vcs:       git,
		review:    review,
		reviewURL: reviewURL,
		cache:     map[string]*rietveld.Issue{},
	}
}

// See the TileBuilder interface.
func (b *tileBuilder) ListLong(begin, end time.Time, source string) ([]*CommitIDLong, error) {
	commitIDs, err := b.db.List(begin, end)
	if err != nil {
		return nil, fmt.Errorf("Error while looking up commits: %s", err)
	}
	return b.convertToLongCommits(commitIDs, source), nil
}

// See the TileBuilder interface.
func (b *tileBuilder) TileFromCommits(commitIDs []*CommitIDLong) (*tiling.Tile, error) {
	shortCids := make([]*CommitID, len(commitIDs))
	for idx, cid := range commitIDs {
		shortCids[idx] = cid.CommitID
	}
	tile, _, err := b.db.TileFromCommits(shortCids)
	return tile, err
}

// convertToLongCommits converts the CommitIDs into CommitIDLong's, after
// potentially filtering the slice based on the provided source.
func (b *tileBuilder) convertToLongCommits(commitIDs []*CommitID, source string) []*CommitIDLong {
	// Filter
	if source != "" {
		dst := []*CommitID{}
		for _, cid := range commitIDs {
			if cid.Source == source {
				dst = append(dst, cid)
			}
		}
		commitIDs = dst
	}

	// Convert to CommitIDLong.
	results := []*CommitIDLong{}
	for _, cid := range commitIDs {
		results = append(results, &CommitIDLong{
			CommitID: cid,
		})
	}

	// Populate Author and Desc from gitinfo or rietveld as appropriate.
	// Caching Rietveld info as needed.
	for _, c := range results {
		if strings.HasPrefix(c.Source, b.reviewURL) {
			// Rietveld
			issueInfo, err := b.getIssue(c.Source)
			if err != nil {
				glog.Errorf("Failed to get details for commit from Rietveld %s: %s", c.ID, err)
				continue
			}
			c.Author = issueInfo.Owner
			c.Desc = issueInfo.Subject
		} else {
			// vcsinfo
			details, err := b.vcs.Details(c.ID, true)
			if err != nil {
				glog.Errorf("Failed to get details for commit from Git %s: %s", c.ID, err)
				continue
			}
			c.Author = details.Author
			c.Desc = details.Subject
		}
	}

	return results
}

// getIssue parses the source, which looks like
// "https://chromium.codereview.org/1232143243" and returns information about
// the issue from Rietveld.
func (b *tileBuilder) getIssue(source string) (*rietveld.Issue, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse trybot source: %s", err)
	}
	// The issue id as a string is the URL path w/o the leading slash.
	issueStr := u.Path[1:]
	if issue, ok := b.cache[issueStr]; !ok {
		issueInt, err := strconv.Atoi(issueStr)
		if err != nil {
			return nil, fmt.Errorf("Unable to convert Rietveld issue id: %s", err)
		}
		issue, err = b.review.GetIssueProperties(int64(issueInt), false)
		if err != nil {
			return nil, fmt.Errorf("Failed to get details for review %s: %s", source, err)
		}
		if len(b.cache) > MAX_CACHE_SIZE {
			b.cache = map[string]*rietveld.Issue{}
		}
		b.cache[issueStr] = issue
		return issue, nil
	} else {
		return issue, nil
	}
}