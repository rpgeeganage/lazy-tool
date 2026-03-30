package search

// ScoreWeights tune hybrid lexical + vector contribution before ranking.
type ScoreWeights struct {
	ExactCanonical   float64
	ExactName        float64
	Substring        float64
	VectorMultiplier float64
	// UserSummary adds a small fixed boost when the user edited summary (explainable; P1.3).
	UserSummary float64
	// Favorite boosts pinned capabilities (P2.3).
	Favorite float64
	// InvocationBoost scales log2(1+invoke_count) for tools with prior invocations.
	InvocationBoost float64
}

// DefaultScoreWeights matches the original hard-coded hybrid scoring.
func DefaultScoreWeights() ScoreWeights {
	return ScoreWeights{
		ExactCanonical:   10,
		ExactName:        8,
		Substring:        2,
		VectorMultiplier: 6,
		UserSummary:      0.25,
		Favorite:         0.2,
		InvocationBoost:  0.5,
	}
}

// MergeScoreWeights returns defaults for any non-positive field.
func MergeScoreWeights(w ScoreWeights) ScoreWeights {
	d := DefaultScoreWeights()
	if w.ExactCanonical > 0 {
		d.ExactCanonical = w.ExactCanonical
	}
	if w.ExactName > 0 {
		d.ExactName = w.ExactName
	}
	if w.Substring > 0 {
		d.Substring = w.Substring
	}
	if w.VectorMultiplier > 0 {
		d.VectorMultiplier = w.VectorMultiplier
	}
	if w.UserSummary > 0 {
		d.UserSummary = w.UserSummary
	}
	if w.Favorite > 0 {
		d.Favorite = w.Favorite
	}
	if w.InvocationBoost > 0 {
		d.InvocationBoost = w.InvocationBoost
	}
	return d
}
