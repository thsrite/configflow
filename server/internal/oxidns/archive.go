package oxidns

import (
	"context"
	"errors"
	"strings"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// BuildTypedZIP materializes a complete OxiDNS package through the shared bounded
// MosDNS rule materializer. Runtime state and external includes are never packed.
// config is the captured repository snapshot; neither it nor the input files change.
func BuildTypedZIP(ctx context.Context, config *model.Config, content string, downloads []mosdns.Download, custom []File, baseURL string) ([]byte, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(downloads)+len(custom))
	for _, item := range downloads {
		p, err := mosdns.ArchivePath(item.LocalPath)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	for _, item := range custom {
		p, err := mosdns.ArchivePath(item.Path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	for _, p := range paths {
		if strings.HasPrefix(p, "state/") {
			return nil, &mosdns.ArchiveError{Message: messageArchiveState}
		}
	}
	document, err := yamlx.Parse([]byte(content))
	if err != nil {
		return nil, err
	}
	if document.Kind != yaml.MappingNode {
		return nil, &mosdns.ArchiveError{Message: messageArchiveFormat}
	}
	// The resource validator operates on effective values. Retain arbitrary YAML
	// keys and tags while resolving aliases/merges with the shared AST facade.
	yamlx.MaterializeAliases(document)
	if externalIncludes(document) {
		return nil, &mosdns.ArchiveError{Message: messageArchiveInclude}
	}
	if err := ValidateResourceFiles(document, paths); err != nil {
		if message, ok := IsSettingsError(err); ok {
			return nil, &mosdns.ArchiveError{Message: message}
		}
		return nil, err
	}
	files := make([]mosdns.File, 0, len(custom))
	for _, f := range custom {
		files = append(files, mosdns.File{Path: f.Path, Content: string(f.Content)})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output, err := mosdns.BuildZIP(ctx, config, content, downloads, files, mosdns.ArchiveOptions{BaseURL: baseURL, SkipReferenceValidation: true})
	if stop := ctx.Err(); stop != nil {
		return nil, stop
	}
	var failure *mosdns.ArchiveError
	if errors.As(err, &failure) {
		return nil, &mosdns.ArchiveError{Message: strings.ReplaceAll(failure.Message, "MosDNS", "OxiDNS")}
	}
	return output, err
}
