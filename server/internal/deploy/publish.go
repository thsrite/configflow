package deploy

// PublishOptions captures activation, snapshot revision and the original publication origin.
type PublishOptions struct {
	Activate                       bool
	ConfigRevision                 *Revision
	BaseURL                        string
	ResourceConfig, SourceSnapshot *Source
}
