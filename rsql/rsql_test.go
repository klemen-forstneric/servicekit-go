package rsql_test

import (
	"testing"
	"time"

	"github.com/klemen-forstneric/servicekit-go/rsql"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestRSQL(t *testing.T) {
	suite.Run(t, new(rsqlSuite))
}

type rsqlSuite struct {
	suite.Suite
	schema rsql.Schema
	limits rsql.Limits
}

func (s *rsqlSuite) SetupTest() {
	s.schema = rsql.Schema{
		"id":           {Column: "_id", Kind: rsql.KindString},
		"creator_id":   {Column: "creator_id", Kind: rsql.KindString},
		"media_format": {Column: "media_format", Kind: rsql.KindString, Enum: []string{"image", "video", "audio"}},
		"rating":       {Column: "rating", Kind: rsql.KindString, Enum: []string{"sfw", "mature", "nsfw", "adult"}},
		"tags":         {Column: "tags", Kind: rsql.KindString},
		"create_time":  {Column: "created_at", Kind: rsql.KindTime},
		"price":        {Column: "price", Kind: rsql.KindInt},
	}
	s.limits = rsql.Limits{MaxDepth: 5, MaxDisjunctions: 20, MaxComparisons: 30}
}

func (s *rsqlSuite) bind(filter string) (rsql.Bound, error) {
	return bindFilter(s.schema, s.limits, filter)
}

func bindFilter(schema rsql.Schema, limits rsql.Limits, filter string) (rsql.Bound, error) {
	return rsql.Bind(filter, schema, limits)
}

// mustCanonical binds and canonicalises in one step. Canonical form is the
// assertion vehicle for structure: it is total, stable and readable, which
// makes it a better oracle than reaching into node types.
func (s *rsqlSuite) mustCanonical(filter string) string {
	b, err := s.bind(filter)
	s.Require().NoError(err, filter)
	return rsql.Canonical(b)
}

func (s *rsqlSuite) TestPrecedenceAndGrouping() {
	// RSQL binds ';' (AND) tighter than ',' (OR) — the conventional way round.
	s.Equal(
		s.mustCanonical("(creator_id==c1;rating==sfw),creator_id==c2"),
		s.mustCanonical("creator_id==c1;rating==sfw,creator_id==c2"),
	)
	s.NotEqual(
		s.mustCanonical("creator_id==c1;(rating==sfw,creator_id==c2)"),
		s.mustCanonical("creator_id==c1;rating==sfw,creator_id==c2"),
	)
}

func (s *rsqlSuite) TestCanonicalIsOrderIndependent() {
	s.Equal(
		s.mustCanonical(`tags=="a";tags=="b"`),
		s.mustCanonical(`tags=="b";tags=="a"`),
	)
	s.Equal(
		s.mustCanonical("creator_id=in=(c2,c1)"),
		s.mustCanonical("creator_id=in=(c1,c2)"),
	)
}

func (s *rsqlSuite) TestOperatorAliasesCollapse() {
	s.Equal(s.mustCanonical("price=gt=5"), s.mustCanonical("price>5"))
	s.Equal(s.mustCanonical("price=le=5"), s.mustCanonical("price<=5"))
	s.Equal(s.mustCanonical("price=ne=5"), s.mustCanonical("price!=5"))
	s.Equal(s.mustCanonical("creator_id=out=(c1)"), s.mustCanonical("creator_id=nin=(c1)"))
}

func (s *rsqlSuite) TestValuesWithReservedCharacters() {
	// The defect that sinks the string-splitting implementations: a value
	// containing a separator, a quote or an '=' must survive intact.
	for _, tc := range []struct{ filter, want string }{
		{`tags=="a,b"`, "a,b"},
		{`tags=="a;b"`, "a;b"},
		{`tags=="a=b"`, "a=b"},
		{`tags=="with space"`, "with space"},
		{`tags==a\,b`, "a,b"},
		{`tags=='single'`, "single"},
		{`tags=="esc\"aped"`, `esc"aped`},
	} {
		b, err := s.bind(tc.filter)
		s.Require().NoError(err, tc.filter)
		cmp, ok := b.(*rsql.BoundComparison)
		s.Require().True(ok, tc.filter)
		s.Equal(tc.want, cmp.Values[0], tc.filter)
	}
}

func (s *rsqlSuite) TestTypeCoercion() {
	b, err := s.bind("create_time=lt=2026-08-12T00:00:00Z")
	s.Require().NoError(err)
	cmp := b.(*rsql.BoundComparison)
	s.Equal(time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), cmp.Values[0])

	b, err = s.bind("price=ge=500")
	s.Require().NoError(err)
	s.Equal(int64(500), b.(*rsql.BoundComparison).Values[0])
}

func (s *rsqlSuite) TestRejects() {
	for name, filter := range map[string]string{
		"undeclared field":      "prompt==x",
		"mongo operator as key": "$where==1",
		"dotted path":           "review.status==passed",
		"bad enum value":        "rating==banana",
		"bad timestamp":         "create_time=lt=yesterday",
		"bad int":               "price=gt=cheap",
		"ordering on string":    "creator_id=gt=c1",
		"ordering on repeated":  `tags=gt="a"`,
		"multi-arg on equality": "creator_id==(c1,c2)",
		"unterminated quote":    `tags=="a`,
		"unbalanced paren":      "(creator_id==c1",
		"trailing operator":     "creator_id==",
		"empty":                 "",
		"junk":                  ";;",
	} {
		_, err := s.bind(filter)
		s.Require().Error(err, "%s: %q should not bind", name, filter)
	}
}

func (s *rsqlSuite) TestLimits() {
	s.limits = rsql.Limits{MaxDepth: 2}
	_, err := s.bind("creator_id==c1;(rating==sfw,(media_format==image;price>1))")
	s.Require().ErrorContains(err, "nests deeper")

	s.limits = rsql.Limits{MaxDisjunctions: 2}
	_, err = s.bind("creator_id==c1,creator_id==c2,creator_id==c3,creator_id==c4")
	s.Require().ErrorContains(err, "disjunctions")

	s.limits = rsql.Limits{MaxComparisons: 2}
	_, err = s.bind("creator_id==c1;rating==sfw;media_format==image")
	s.Require().ErrorContains(err, "comparisons")
}

func (s *rsqlSuite) TestDavesQuery() {
	b, err := s.bind(`tags=="category:profile";tags=="category:joi";` +
		`(media_format==video,media_format==audio);` +
		`create_time=lt=2026-08-12T00:00:00Z;` +
		`creator_id=in=(1,2,3)`)
	s.Require().NoError(err)
	s.Require().NotNil(b)
	require.NotEmpty(s.T(), rsql.Canonical(b))
}

func (s *rsqlSuite) TestColumnDefaultsToSelector() {
	schema := rsql.Schema{"rating": {Kind: rsql.KindString}}
	b, err := bindFilter(schema, rsql.Limits{}, "rating==sfw")
	s.Require().NoError(err)
	s.Equal("rating", b.(*rsql.BoundComparison).Field.Column,
		"a forgotten Column must not translate to an empty query key")
}

// Canonical is echoed to callers as the filter the server parsed, so it has to
// survive a round trip through the parser.
func (s *rsqlSuite) TestCanonicalRoundTrips() {
	for _, filter := range []string{
		`tags=="a,b"`,
		`tags=="with space"`,
		`tags=="quote\"inside"`,
		`creator_id==c1;rating==sfw`,
		`creator_id==c1;rating==sfw,media_format==video`,
		`creator_id=in=(c1,c2)`,
		`create_time=lt=2026-08-12T00:00:00Z`,
	} {
		first := s.mustCanonical(filter)
		s.Equal(first, s.mustCanonical(first), "canonical form of %q must re-parse to itself", filter)
	}
}
