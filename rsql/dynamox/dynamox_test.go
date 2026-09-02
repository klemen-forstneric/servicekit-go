package dynamox_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/stretchr/testify/suite"

	"github.com/klemen-forstneric/servicekit-go/rsql"
	"github.com/klemen-forstneric/servicekit-go/rsql/dynamox"
)

func TestDynamox(t *testing.T) {
	suite.Run(t, new(dynamoxSuite))
}

type dynamoxSuite struct {
	suite.Suite
	schema rsql.Schema
}

func (s *dynamoxSuite) SetupTest() {
	s.schema = rsql.Schema{
		"creator_id": {Column: "creator_id", Kind: rsql.KindString},
		"hide":       {Column: "hide", Kind: rsql.KindBool},
		"ready":      {Column: "ready", Kind: rsql.KindBool},
		"kinks":      {Column: "kinks", Kind: rsql.KindString, Repeated: true},
		"joined_at":  {Column: "joined_at", Kind: rsql.KindTime},
	}
}

// render builds the condition and asks the AWS builder to serialise it, so the
// assertions are against what Dynamo would actually receive rather than against
// the builder's internal shape.
func (s *dynamoxSuite) render(expr string) (string, map[string]string, int) {
	n, err := rsql.Parse(expr)
	s.Require().NoError(err, expr)
	b, err := rsql.Bind(n, s.schema, rsql.Limits{})
	s.Require().NoError(err, expr)
	cond, err := dynamox.ToCondition(b)
	s.Require().NoError(err, expr)
	built, err := expression.NewBuilder().WithFilter(cond).Build()
	s.Require().NoError(err, expr)
	return *built.Filter(), built.Names(), len(built.Values())
}

func (s *dynamoxSuite) TestComparisons() {
	f, names, vals := s.render("hide==false")
	s.Equal("#0 = :0", f)
	s.Equal(map[string]string{"#0": "hide"}, names)
	s.Equal(1, vals)

	f, _, _ = s.render("hide==false;ready==true")
	s.Equal("(#0 = :0) AND (#1 = :1)", f)

	f, _, _ = s.render("creator_id=in=(c1,c2)")
	s.Equal("#0 IN (:0, :1)", f)
}

// Dynamo has no equality against a set, so a repeated attribute must become
// contains(), and a union of them must become an OR of contains().
func (s *dynamoxSuite) TestRepeatedFieldBecomesContains() {
	f, names, _ := s.render(`kinks=="feet"`)
	s.Equal("contains (#0, :0)", f)
	s.Equal(map[string]string{"#0": "kinks"}, names)

	f, _, vals := s.render(`kinks=in=(feet,bdsm)`)
	s.Equal("(contains (#0, :0)) OR (contains (#0, :1))", f)
	s.Equal(2, vals)

	f, _, _ = s.render(`kinks!="feet"`)
	s.Equal("NOT (contains (#0, :0))", f)
}

func (s *dynamoxSuite) TestOrderingOnRepeatedIsRejectedBeforeTranslation() {
	n, err := rsql.Parse(`kinks=gt="a"`)
	s.Require().NoError(err)
	_, err = rsql.Bind(n, s.schema, rsql.Limits{})
	s.Require().ErrorContains(err, "not allowed")
}

func (s *dynamoxSuite) TestPrecedenceSurvivesTranslation() {
	f, _, _ := s.render("hide==false;ready==true,creator_id==c1")
	s.Equal("((#0 = :0) AND (#1 = :1)) OR (#2 = :2)", f)
}
