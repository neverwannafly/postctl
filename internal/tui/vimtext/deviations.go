package vimtext

// differentialDeviationReasons is intentionally short. The differential test
// maps cases to these stable categories and reports every allowed mismatch.
var differentialDeviationReasons = map[string]string{
	"literal-search": "vimtext search is literal; nvim search uses regular expressions",
}
