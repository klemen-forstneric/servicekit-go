package mongox_test

import (
	"time"

	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/klemen-forstneric/servicekit-go/rsql"
	"github.com/klemen-forstneric/servicekit-go/rsql/mongox"
)

func TestMongox(t *testing.T) {
	suite.Run(t, new(mongoxSuite))
}

type mongoxSuite struct {
	suite.Suite
	schema rsql.Schema
}

func (s *mongoxSuite) SetupTest() {
	s.schema = rsql.Schema{
		"creator_id":  {Column: "creator_id", Kind: rsql.KindString},
		"rating":      {Column: "rating", Kind: rsql.KindString, Enum: []string{"sfw", "nsfw"}},
		"tags":        {Column: "tags", Kind: rsql.KindString, Repeated: true},
		"create_time": {Column: "created_at", Kind: rsql.KindTime},
		"price":       {Column: "price", Kind: rsql.KindInt},
	}
}

func (s *mongoxSuite) filter(expr string) mongox.Filter {
	n, err := rsql.Parse(expr)
	s.Require().NoError(err, expr)
	b, err := rsql.Bind(n, s.schema, rsql.Limits{})
	s.Require().NoError(err, expr)
	f, err := mongox.ToFilter(b)
	s.Require().NoError(err, expr)
	return f
}

func (s *mongoxSuite) TestComparisons() {
	s.Equal(mongox.Filter{"creator_id": "c1"}, s.filter("creator_id==c1"))
	s.Equal(mongox.Filter{"creator_id": mongox.Filter{"$ne": "c1"}}, s.filter("creator_id!=c1"))
	s.Equal(mongox.Filter{"price": mongox.Filter{"$gte": int64(500)}}, s.filter("price=ge=500"))
	s.Equal(mongox.Filter{"creator_id": mongox.Filter{"$in": []any{"c1", "c2"}}}, s.filter("creator_id=in=(c1,c2)"))
}

// Mongo already treats equality against an array as "contains", so the
// repeated case needs no special translation — this pins that assumption.
func (s *mongoxSuite) TestRepeatedFieldNeedsNoSpecialCase() {
	s.Equal(mongox.Filter{"tags": "category:joi"}, s.filter(`tags=="category:joi"`))
	s.Equal(
		mongox.Filter{"$and": []mongox.Filter{{"tags": "a"}, {"tags": "b"}}},
		s.filter(`tags=="a";tags=="b"`),
	)
	s.Equal(mongox.Filter{"tags": mongox.Filter{"$in": []any{"a", "b"}}}, s.filter(`tags=in=(a,b)`))
}

func (s *mongoxSuite) TestPrecedenceSurvivesTranslation() {
	// ';' binds tighter than ',', so this is (creator AND rating) OR creator.
	s.Equal(
		mongox.Filter{"$or": []mongox.Filter{
			{"$and": []mongox.Filter{{"creator_id": "c1"}, {"rating": "sfw"}}},
			{"creator_id": "c2"},
		}},
		s.filter("creator_id==c1;rating==sfw,creator_id==c2"),
	)
}

func (s *mongoxSuite) TestTimestampStaysTyped() {
	f := s.filter("create_time=lt=2026-08-12T00:00:00Z")
	inner, ok := f["created_at"].(mongox.Filter)
	s.Require().True(ok)
	s.IsType(time.Time{}, inner["$lt"], "a date must reach the driver as time.Time, not a string")
}
