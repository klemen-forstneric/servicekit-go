package rsql_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/klemen-forstneric/servicekit-go/rsql"
)

const (
	storeMongo  = "mongo"
	storeDynamo = "dynamo"
)

func TestSplit(t *testing.T) {
	suite.Run(t, new(splitSuite))
}

type splitSuite struct {
	suite.Suite
	schema rsql.Schema
}

func (s *splitSuite) SetupTest() {
	// creator_id is declared against mongo because that is where the assets
	// live. Reaching the creator record for the same id is a staging step, not
	// a second declaration — see TestStagedAcrossStores.
	s.schema = rsql.Schema{
		"creator_id":   {Column: "creator_id", Kind: rsql.KindString, Store: storeMongo},
		"rating":       {Column: "rating", Kind: rsql.KindString, Store: storeMongo},
		"media_format": {Column: "media_format", Kind: rsql.KindString, Store: storeMongo},
		"creator_kink": {Column: "kinks", Kind: rsql.KindString, Repeated: true, Store: storeDynamo},
		"creator_hide": {Column: "hide", Kind: rsql.KindBool, Store: storeDynamo},
	}
}

func (s *splitSuite) bind(filter string) rsql.Bound {
	b, err := bindFilter(s.schema, rsql.Limits{}, filter)
	s.Require().NoError(err, filter)
	return b
}

func (s *splitSuite) TestConjunctionSplits() {
	parts, err := rsql.Split(s.bind(`creator_kink=="feet";rating==sfw;media_format==video`))
	s.Require().NoError(err)
	s.Require().Len(parts, 2)
	s.Equal("creator_kink==feet", rsql.Canonical(parts[storeDynamo]))
	s.Equal("(media_format==video;rating==sfw)", rsql.Canonical(parts[storeMongo]))
}

func (s *splitSuite) TestSingleStoreStaysWhole() {
	parts, err := rsql.Split(s.bind("rating==sfw,rating==mature"))
	s.Require().NoError(err)
	s.Require().Len(parts, 1)
	s.NotNil(parts[storeMongo])
}

// A single-store disjunction nested under a conjunction is fine: it still has
// to hold, so it can be handed to its own store whole.
func (s *splitSuite) TestNestedSingleStoreDisjunctionSplits() {
	parts, err := rsql.Split(s.bind(`creator_hide==false;(rating==sfw,media_format==video)`))
	s.Require().NoError(err)
	s.Equal("creator_hide==false", rsql.Canonical(parts[storeDynamo]))
	s.Equal("(media_format==video,rating==sfw)", rsql.Canonical(parts[storeMongo]))
}

func (s *splitSuite) TestCrossStoreDisjunctionIsRejected() {
	for _, filter := range []string{
		`creator_kink=="feet",rating==sfw`,
		`media_format==video;(creator_hide==false,rating==sfw)`,
	} {
		_, err := rsql.Split(s.bind(filter))
		s.Require().ErrorContains(err, "spans stores", filter)
	}
}

// TestStagedAcrossStores is the whole point: resolve the store that gates
// visibility first, then narrow the filter for the store that holds the rows.
func (s *splitSuite) TestStagedAcrossStores() {
	b := s.bind(`creator_id=in=(c1,c2,c3);rating==sfw`)

	// Stage 1 — read what the caller asked for, without translating anything.
	requested := rsql.Comparisons(b, "creator_id")
	s.Require().Len(requested, 1)
	s.Equal([]any{"c1", "c2", "c3"}, requested[0].Values)

	// ...which a creator repository resolves against Dynamo. c2 is hidden.
	visible := []string{"c1", "c3"}

	// Stage 2 — narrow the existing predicate rather than conjoining a second
	// one, so the query Mongo receives has a single creator_id clause.
	narrowed, err := s.schema.Comparison("creator_id", rsql.OpIn, visible...)
	s.Require().NoError(err)
	staged := rsql.Rewrite(b, func(n rsql.Bound) rsql.Bound {
		if c, ok := n.(*rsql.BoundComparison); ok && c.Selector == "creator_id" {
			return narrowed
		}
		return n
	})
	s.Equal("(creator_id=in=(c1,c3);rating==sfw)", rsql.Canonical(staged))
}

func (s *splitSuite) TestRewriteDroppingAPredicateCollapsesTheParent() {
	b := s.bind("creator_id==c1;rating==sfw")
	stripped := rsql.Rewrite(b, func(n rsql.Bound) rsql.Bound {
		if c, ok := n.(*rsql.BoundComparison); ok && c.Selector == "rating" {
			return nil
		}
		return n
	})
	s.Equal("creator_id==c1", rsql.Canonical(stripped), "a one-armed conjunction must collapse")
}

func (s *splitSuite) TestServerPredicateCannotBeExpressedByACaller() {
	// hide is declared, so a caller could filter on it; eligibility is enforced
	// by conjoining the server's own predicate, which no filter can widen.
	gate, err := s.schema.Comparison("creator_hide", rsql.OpEq, "false")
	s.Require().NoError(err)
	guarded := rsql.All(s.bind("rating==sfw"), gate)
	s.Equal("(creator_hide==false;rating==sfw)", rsql.Canonical(guarded))

	_, err = s.schema.Comparison("prompt", rsql.OpEq, "x")
	s.Require().ErrorContains(err, "not a filterable field")
}

func (s *splitSuite) TestAllAndAnyFlatten() {
	a, b, c := s.bind("rating==sfw"), s.bind("media_format==video"), s.bind("creator_id==c1")
	s.Equal("(creator_id==c1;media_format==video;rating==sfw)", rsql.Canonical(rsql.All(rsql.All(a, b), c)))
	s.Equal("(creator_id==c1,media_format==video,rating==sfw)", rsql.Canonical(rsql.Any(rsql.Any(a, b), c)))
	s.Nil(rsql.All())
	s.Equal(a, rsql.All(nil, a, nil))
	// An OR nested in an AND must not be flattened away. Canonical sorts
	// operands, and "(" sorts before a column name, so the group leads.
	s.Equal("((media_format==video,rating==sfw);creator_id==c1)", rsql.Canonical(rsql.All(rsql.Any(a, b), c)))
}
