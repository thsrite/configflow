package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

type client struct {
	service            *Service
	base               *url.URL
	username, password string
	secrets            []string
}

func (s *Service) client(settings Settings) (*client, error) {
	if strings.TrimRight(settings.URL, "/") == "" || settings.Username == "" || settings.Password == "" {
		return nil, ErrIncompleteSettings
	}
	base, err := url.Parse(strings.TrimRight(settings.URL, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("invalid WebDAV address")
	}
	secrets := []string{settings.Username, settings.Password}
	if base.User != nil {
		password, _ := base.User.Password()
		secrets = append(secrets, base.User.Username(), password)
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	base.User = nil
	base.RawQuery = ""
	base.Fragment = ""
	return &client{service: s, base: base, username: settings.Username, password: settings.Password, secrets: secrets}, nil
}
func (c *client) request(ctx context.Context, method, remote string, data []byte) (*http.Response, error) {
	target := *c.base
	target.Path = strings.TrimRight(target.Path, "/") + remote
	target.RawPath = ""
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(data))
	if err != nil {
		cancel()
		return nil, errors.New("invalid WebDAV request")
	}
	req.SetBasicAuth(c.username, c.password)
	if method == "PROPFIND" {
		req.Header.Set("Depth", "1")
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	if method == "PUT" {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.service.HTTP.Do(req)
	if err != nil {
		cancel()
		return nil, &transportError{err}
	}
	response.Body = &cancelBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error { err := b.ReadCloser.Close(); b.cancel(); return err }
func (c *client) check(ctx context.Context, remote string) (bool, error) {
	response, err := c.request(ctx, "HEAD", remote, nil)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	// webdav3.check treats HTTP errors as a missing resource. Connection failures
	// remain errors; test/root failures therefore retain the same 400/500 split.
	if response.StatusCode == http.StatusInsufficientStorage {
		return false, &StatusError{response.StatusCode}
	}
	if response.StatusCode == http.StatusMethodNotAllowed {
		return false, &StatusError{response.StatusCode}
	}
	return response.StatusCode == http.StatusOK, nil
}
func (c *client) mkdir(ctx context.Context, remote string) error {
	parent := path.Dir(strings.TrimRight(remote, "/")) + "/"
	if parent == "//" {
		parent = "/"
	}
	exists, err := c.check(ctx, parent)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("parent directory unavailable")
	}
	response, err := c.request(ctx, "MKCOL", remote, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusMethodNotAllowed {
		return nil
	}
	if response.StatusCode >= 400 {
		return &StatusError{response.StatusCode}
	}
	return nil
}
func (c *client) ensure(ctx context.Context, directory string) error {
	exists, err := c.check(ctx, directory)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	current := "/"
	for _, part := range strings.Split(strings.Trim(directory, "/"), "/") {
		if part == "" {
			continue
		}
		current += part + "/"
		exists, err := c.check(ctx, current)
		if err != nil {
			return err
		}
		if !exists {
			if err := c.mkdir(ctx, current); err != nil {
				return &DirectoryError{c.safePath(current), err}
			}
		}
	}
	return nil
}
func (c *client) put(ctx context.Context, remote string, data []byte) error {
	parent := path.Dir(remote) + "/"
	if parent == "//" {
		parent = "/"
	}
	exists, err := c.check(ctx, parent)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("parent directory unavailable")
	}
	response, err := c.request(ctx, "PUT", remote, data)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return &StatusError{response.StatusCode}
	}
	return nil
}
func (c *client) list(ctx context.Context, directory string) ([]string, error) {
	if directory != "/" {
		exists, err := c.check(ctx, directory)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errors.New("directory unavailable")
		}
	}
	response, err := c.request(ctx, "PROPFIND", directory, []byte(`<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>`))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, &StatusError{response.StatusCode}
	}
	var listing struct {
		Responses []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&listing); err != nil {
		return nil, errors.New("invalid WebDAV directory response")
	}
	names := []string{}
	expected := strings.TrimRight(c.base.Path, "/") + directory
	for _, entry := range listing.Responses {
		u, err := url.Parse(entry.Href)
		if err != nil {
			continue
		}
		remote := u.Path
		if strings.TrimRight(remote, "/") == strings.TrimRight(expected, "/") {
			continue
		}
		name := path.Base(strings.TrimRight(remote, "/"))
		if strings.HasSuffix(remote, "/") {
			name += "/"
		}
		names = append(names, name)
	}
	return names, nil
}
func (c *client) remove(ctx context.Context, remote string) error {
	response, err := c.request(ctx, "DELETE", remote, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return &StatusError{response.StatusCode}
	}
	return nil
}

// Test checks the root, then recursively creates the requested directory.
func (s *Service) Test(ctx context.Context, settings Settings) (bool, error) {
	c, err := s.client(settings)
	if err != nil {
		return false, err
	}
	exists, err := c.check(ctx, "/")
	if err != nil || !exists {
		return false, err
	}
	directory := settings.Path
	if !strings.HasPrefix(directory, "/") {
		directory = "/" + directory
	}
	if !strings.HasSuffix(directory, "/") {
		directory += "/"
	}
	return true, c.ensure(ctx, directory)
}

func (c *client) safePath(value string) string {
	original := value
	redacted := false
	for range 8 {
		for _, secret := range c.secrets {
			if secret != "" && strings.Contains(value, secret) {
				value = strings.ReplaceAll(value, secret, "[REDACTED]")
				redacted = true
			}
		}
		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return "[REDACTED]"
		}
		if decoded == value {
			if redacted {
				return value
			}
			return original
		}
		value = decoded
	}
	return "[REDACTED]"
}

type transportError struct{ cause error }

func (e *transportError) Error() string { return DescribeError(e.cause) }
func (e *transportError) Unwrap() error { return e.cause }
