package hermes

// AnalysisResponse is the structured JSON returned by the Hermes AI analyzer.
type AnalysisResponse struct {
	IsRequest             bool     `json:"is_request"`
	RequestType           string   `json:"request_type"`
	Client                string   `json:"client"`
	Project               string   `json:"project"`
	Relevance             string   `json:"relevance"`
	Risk                  string   `json:"risk"`
	NeedsClarification    bool     `json:"needs_clarification"`
	ClarificationQuestions []string `json:"clarification_questions"`
	Summary               string   `json:"summary"`
	Recommendation        string   `json:"recommendation"`
	NextAction            string   `json:"next_action"`
}
