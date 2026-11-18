package dnsha

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/model"
)

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func childID(owner, did, id string) string {
	if owner == id {
		return did
	}
	return "ha-" + digest([]byte(owner + "/" + did + "/" + id))[:48]
}
func keys(j *model.HAJournal) []string {
	if j == nil {
		return nil
	}
	ids := j.Children.Keys()
	slices.Sort(ids)
	return ids
}
func (s *Service) folder(j *model.HAJournal) string {
	return filepath.Join(s.m.Repository().DataDir(), ".oxidns-ha-publications", digest([]byte(strings.Join(keys(j), "\n"))))
}
func (s *Service) lease(ctx context.Context, ids []string) (context.Context, func(), bool) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	ks := []string{"pair:" + strings.Join(ids, "\n")}
	for _, id := range ids {
		ks = append(ks, "member:"+id)
	}
	releases := []func(){}
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, k := range ks {
		held, done, ok := pairLocks.TryLock(ctx, s.m.Repository().DataDir()+"\x00"+k)
		if !ok {
			release()
			return ctx, func() {}, false
		}
		ctx = held
		releases = append(releases, done)
	}
	return ctx, release, true
}
func (s *Service) writePayload(ctx context.Context, j *model.HAJournal, p *payload, b *deploy.Bundle) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	folder := s.folder(j)
	if e := os.MkdirAll(folder, 0700); e != nil {
		return fmt.Errorf("create HA publication directory: %w", e)
	}
	if b != nil {
		archive, e := b.Archive()
		if e != nil {
			return e
		}
		sha, e := b.SHA256()
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = deploy.WriteFileAtomic(ctx, filepath.Join(folder, "bundle.tar.gz"), archive); e != nil {
			return e
		}
		p.Bundle = &bundleMeta{b.Manifest(), sha, b.ConfigVersion(), b.Comparison()}
	}
	p.ID = j.ID
	data, e := json.Marshal(p)
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return deploy.WriteFileAtomic(ctx, filepath.Join(folder, "payload.json"), data)
}
func (s *Service) readPayload(ctx context.Context, j *model.HAJournal) (*payload, *deploy.Bundle, error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	data, e := os.ReadFile(filepath.Join(s.folder(j), "payload.json"))
	if e != nil {
		return nil, nil, e
	}
	var p payload
	if e = json.Unmarshal(data, &p); e != nil {
		return nil, nil, e
	}
	if j.ID == "" || p.ID == "" || p.ID != j.ID {
		return nil, nil, refuse(msgPayloadID)
	}
	if p.Bundle == nil {
		return &p, nil, nil
	}
	data, e = os.ReadFile(filepath.Join(s.folder(j), "bundle.tar.gz"))
	if e != nil {
		return nil, nil, e
	}
	meta := p.Bundle
	if digest(data) != meta.SHA256 {
		return nil, nil, refuse(msgPayloadDigest)
	}
	b, e := deploy.Open(meta.Manifest, data, meta.SHA256, meta.ConfigVersion, meta.Comparison)
	if e != nil {
		return nil, nil, refuse(msgPayloadDigest)
	}
	return &p, b, nil
}
func (s *Service) save(ctx context.Context, j *model.HAJournal) error {
	if j.ID == "" || j.Children.Len() != 2 {
		return refuse(msgJournalChanged)
	}
	j.UpdatedAt = model.Int(s.now().Unix())
	return s.update(ctx, func(list []*agent) error {
		order := slices.Clone(j.Order)
		if len(order) == 0 {
			if j.Enable != nil {
				order = []string{j.Enable.PeerID, j.OwnerAgentID}
			} else {
				order = j.Children.Keys()
				slices.SortStableFunc(order, func(a, b string) int {
					aa, bb := selected(list, a), selected(list, b)
					secondary := func(v *agent) bool { return haEnabled(v) && v.OxiDNSHA.Role == "secondary" }
					if secondary(aa) && !secondary(bb) {
						return -1
					}
					if !secondary(aa) && secondary(bb) {
						return 1
					}
					return 0
				})
			}
		}
		if len(order) != 2 || order[0] == order[1] {
			return refuse(msgJournalChanged)
		}
		members := []model.HAMember{}
		for _, id := range order {
			a := selected(list, id)
			did, ok := j.Children.Get(id)
			if !ok || did == "" || a == nil || a.OxiDNSHAPublication == nil || a.OxiDNSHAPublication.ID != j.ID {
				return refuse(msgJournalChanged)
			}
			st, ok := a.Deployments.Get(did)
			status := "preparing"
			if ok {
				status = st.Status
			}
			if !yes(st.Submitted) && (j.ActiveAgentID == nil || *j.ActiveAgentID != id) {
				status = "waiting"
				if st.Status == "failed" {
					status = "skipped"
				}
			}
			members = append(members, model.HAMember{AgentID: id, Name: name(a), DeploymentID: did, Status: status})
		}
		j.Members = members
		for id, did := range j.Children.All() {
			a := selected(list, id)
			var copy model.HAJournal
			if e := json.Unmarshal(journalBytes(j), &copy); e != nil {
				return e
			}
			a.OxiDNSHAPublication = &copy
			if st, ok := a.Deployments.Get(did); ok {
				st.HAPublication = summary(j)
				a.Deployments.Set(did, st)
				if a.LatestDeployment != nil && a.LatestDeployment.DeploymentID == did {
					latest := st
					a.LatestDeployment = &latest
				}
			}
		}
		return nil
	})
}
func (s *Service) reserve(ctx context.Context, members []*agent, owner, did string, activate bool, enable *model.HAEnable) (*model.HAJournal, error) {
	id, e := randomID()
	if e != nil {
		return nil, e
	}
	var j model.HAJournal
	// Required nulls and empty order are seeded explicitly; optional fields remain absent.
	if e = json.Unmarshal([]byte(`{"active_agent_id":null,"order":[],"enable":null,"handoff_protocol":1}`), &j); e != nil {
		return nil, e
	}
	j.ID = id
	j.OwnerAgentID = owner
	j.OwnerDeploymentID = did
	j.Status = "preparing"
	j.Phase = "preparing"
	j.Message = msgPreparing
	j.Activate = ptr(activate)
	j.Enable = enable
	j.UpdatedAt = model.Int(s.now().Unix())
	for _, a := range members {
		j.Children.Set(a.ID, childID(owner, did, a.ID))
		j.Identities.Set(a.ID, a.identity)
	}
	e = s.update(ctx, func(list []*agent) error {
		for _, a := range members {
			cur := selected(list, a.ID)
			if cur == nil || !equal(cur.identity, a.identity) || !equal(cur.haRaw, a.haRaw) {
				return refuse(msgIdentityChanged)
			}
			if active(cur) {
				return refuse(msgExisting)
			}
			if cur.Enabled != nil && !*cur.Enabled {
				return refuse(msgMembersDisabled)
			}
			if cur.LatestUpgrade != nil && !childTerminal(cur.LatestUpgrade.Status) {
				return refuse(msgUpgrading)
			}
			for _, st := range cur.Deployments.All() {
				if !childTerminal(st.Status) && !(cur.ID == owner && st.DeploymentID == did) {
					return refuse(msgUnfinished)
				}
			}
		}
		for _, a := range members {
			copy := j
			selected(list, a.ID).OxiDNSHAPublication = &copy
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return &j, nil
}
func (s *Service) members(ctx context.Context, j *model.HAJournal) ([]*agent, error) {
	list, e := s.all(ctx)
	if e != nil {
		return nil, e
	}
	out := []*agent{}
	if j.Children.Len() != 2 {
		return nil, refuse(msgInFlightIdentity)
	}
	for id, did := range j.Children.All() {
		a := selected(list, id)
		identity, ok := j.Identities.Get(id)
		if a == nil || !ok || did == "" || !equal(a.identity, identity) {
			return nil, refuse(msgInFlightIdentity)
		}
		out = append(out, a)
	}
	return out, nil
}
func childBundle(b *deploy.Bundle, id, did string) (*deploy.Bundle, error) {
	m := b.Manifest()
	m.AgentID = id
	m.DeploymentID = did
	files, e := b.Contents()
	if e != nil {
		return nil, e
	}
	return deploy.Pack(m, files, b.ConfigVersion(), b.Comparison())
}
func revisionRaw(r *deploy.Revision) json.RawMessage {
	if r == nil || r.Null {
		return json.RawMessage("null")
	}
	return rawObject(r.Value)
}
func publicationOptions(p *payload) (deploy.PublishOptions, error) {
	o := deploy.PublishOptions{BaseURL: p.BaseURL}
	var e error
	if len(p.Source) > 0 && string(p.Source) != "null" {
		o.SourceSnapshot, e = deploy.SourceFromJSON(p.Source, nil)
		if e != nil {
			return o, e
		}
	}
	if len(p.Resource) > 0 && string(p.Resource) != "null" {
		o.ResourceConfig, e = deploy.SourceFromJSON(p.Resource, nil)
		if e != nil {
			return o, e
		}
	}
	if len(p.Revision) > 0 {
		r := &deploy.Revision{Null: string(p.Revision) == "null"}
		if !r.Null {
			if e = json.Unmarshal(p.Revision, &r.Value); e != nil {
				return o, fmt.Errorf("decode HA revision: %w", e)
			}
		}
		o.ConfigRevision = r
	}
	return o, nil
}

// journalBytes makes explicit nulls and zero counters durable without changing
// the stored-type codec's ordinary omission rules.
func journalBytes(j *model.HAJournal) []byte {
	o := parseObject(rawObject(j))
	if j.ActiveAgentID == nil {
		o["active_agent_id"] = []byte("null")
	}
	if j.Enable == nil {
		o["enable"] = []byte("null")
	}
	if j.Handoff != nil {
		h := parseObject(rawObject(j.Handoff))
		h["confirmations"] = rawObject(j.Handoff.Confirmations)
		o["handoff"] = rawObject(h)
	}
	return rawObject(o)
}
