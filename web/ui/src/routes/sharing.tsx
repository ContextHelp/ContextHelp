import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { Loader2, Share2, Users, FileText, KeyRound, Trash2 } from 'lucide-react';
import { session, sharing, SCOPE_ADMIN_SHARES, type Audience, type SharePolicy, type ShareGrant } from '@/api';

export const Route = createFileRoute('/sharing')({ component: SharingView });

/**
 * The Sharing page (ADR-080 §11): audiences with their members, share
 * policies with their version, grants with their expiry, and the
 * built-in templates. Every control needs admin:shares, which only an
 * admin's session holds; anyone else sees what sharing is and who
 * manages it.
 */
function SharingView() {
  const who = useQuery({ queryKey: ['whoami'], queryFn: () => session.whoami(), retry: false });
  const canManage = who.data?.scopes.includes(SCOPE_ADMIN_SHARES) ?? false;

  return (
    <div className="view">
      <header className="view-header">
        <h1>Sharing</h1>
        <p className="view-subtitle">Who sees what: audiences, share policies and grants</p>
      </header>

      {who.isPending && (
        <div className="loading-row">
          <Loader2 size={16} className="spin" /> Loading…
        </div>
      )}

      {who.isSuccess && !canManage && (
        <section className="panel" data-sharing="read-only">
          <p>
            Sharing is managed by the owner of this instance. What you can see here comes from the audiences you are
            in and the policies granted to them; the banner above names them.
          </p>
          <p className="muted">
            On the owner&apos;s machine: <code>ctxt share audience list</code>, <code>ctxt share policy list</code>.
          </p>
        </section>
      )}

      {canManage && (
        <div className="share-grid" data-sharing="manage">
          <AudiencesPanel />
          <PoliciesPanel />
          <GrantsPanel />
        </div>
      )}
    </div>
  );
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Days from now, as the RFC 3339 time the API takes; undefined for no expiry. */
function expiryFromDays(days: string): string | undefined {
  const n = Number(days);
  if (!days.trim() || !Number.isFinite(n) || n <= 0) return undefined;
  return new Date(Date.now() + n * 24 * 60 * 60 * 1000).toISOString();
}

function fmtDate(s: string) {
  return new Date(s).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function AudiencesPanel() {
  const qc = useQueryClient();
  const audiences = useQuery({ queryKey: ['share', 'audiences'], queryFn: () => sharing.audiences() });
  const [name, setName] = useState('');
  const invalidate = () => qc.invalidateQueries({ queryKey: ['share'] });

  const create = useMutation({ mutationFn: (n: string) => sharing.createAudience(n), onSuccess: () => { setName(''); void invalidate(); } });
  const remove = useMutation({
    mutationFn: (v: { audience: string; principal: string }) => sharing.removeMember(v.audience, v.principal),
    onSuccess: invalidate,
  });

  const onCreate = (e: FormEvent) => {
    e.preventDefault();
    if (name.trim()) create.mutate(name.trim());
  };

  return (
    <section className="panel" data-panel="audiences">
      <div className="panel-header">
        <h2><Users size={14} /> Audiences</h2>
      </div>
      <form className="share-form" onSubmit={onCreate}>
        <input
          className="share-input"
          placeholder="new audience, e.g. partners"
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-label="New audience name"
        />
        <button className="btn btn-primary btn-xs" type="submit" disabled={create.isPending || !name.trim()}>
          Create
        </button>
      </form>
      {create.error && <p className="error-msg">{errorText(create.error)}</p>}
      {audiences.error && <p className="error-msg">{errorText(audiences.error)}</p>}
      {audiences.data?.data.length === 0 && <p className="empty">No audiences yet.</p>}
      <ul className="share-list">
        {audiences.data?.data.map((a) => (
          <AudienceRow key={a.id} audience={a} onRemove={(p) => remove.mutate({ audience: a.id, principal: p })} />
        ))}
      </ul>
      {remove.error && <p className="error-msg">{errorText(remove.error)}</p>}
    </section>
  );
}

function AudienceRow({ audience, onRemove }: { audience: Audience; onRemove: (principal: string) => void }) {
  const qc = useQueryClient();
  const [principal, setPrincipal] = useState('');
  const [days, setDays] = useState('');
  const add = useMutation({
    mutationFn: () => sharing.addMember(audience.id, principal.trim(), expiryFromDays(days)),
    onSuccess: () => { setPrincipal(''); setDays(''); void qc.invalidateQueries({ queryKey: ['share'] }); },
  });
  const now = Date.now();

  return (
    <li className="share-row" data-audience={audience.name}>
      <div className="share-row-head">
        <span className="share-name">{audience.name}</span>
        <span className="obj-id">{audience.id}</span>
      </div>
      <ul className="share-members">
        {audience.members.length === 0 && <li className="muted">No members.</li>}
        {audience.members.map((m) => {
          const expired = m.expires_at !== undefined && new Date(m.expires_at).getTime() < now;
          return (
            <li key={m.principal_id} className="share-member" data-principal={m.principal_id}>
              <span className="font-mono">{m.principal_id}</span>
              {m.expires_at && (
                <span className="muted">{expired ? 'expired' : 'until'} {fmtDate(m.expires_at)}</span>
              )}
              <button className="btn btn-ghost btn-xs" type="button" title="Remove from audience" onClick={() => onRemove(m.principal_id)}>
                <Trash2 size={12} />
              </button>
            </li>
          );
        })}
      </ul>
      <form
        className="share-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (principal.trim()) add.mutate();
        }}
      >
        <input
          className="share-input"
          placeholder="principal id, prn_…"
          value={principal}
          onChange={(e) => setPrincipal(e.target.value)}
          aria-label={`Add a principal to ${audience.name}`}
        />
        <input
          className="share-input share-input--short"
          placeholder="days"
          inputMode="numeric"
          value={days}
          onChange={(e) => setDays(e.target.value)}
          aria-label="Membership expiry in days"
        />
        <button className="btn btn-ghost btn-xs" type="submit" disabled={add.isPending || !principal.trim()}>
          Add
        </button>
      </form>
      {add.error && <p className="error-msg">{errorText(add.error)}</p>}
    </li>
  );
}

function PoliciesPanel() {
  const qc = useQueryClient();
  const policies = useQuery({ queryKey: ['share', 'policies'], queryFn: () => sharing.policies() });
  const templates = useQuery({ queryKey: ['share', 'templates'], queryFn: () => sharing.templates() });
  const [template, setTemplate] = useState('');
  const [document, setDocument] = useState('');
  const [editing, setEditing] = useState<SharePolicy | null>(null);
  const invalidate = () => qc.invalidateQueries({ queryKey: ['share'] });

  const install = useMutation({ mutationFn: (n: string) => sharing.installTemplate(n), onSuccess: invalidate });
  const save = useMutation({
    mutationFn: () => (editing ? sharing.updatePolicy(editing.id, document) : sharing.createPolicy(document)),
    onSuccess: () => { setDocument(''); setEditing(null); void invalidate(); },
  });

  const startEdit = (p: SharePolicy) => {
    setEditing(p);
    setDocument(JSON.stringify({ name: p.name, ...p.spec }, null, 2));
  };

  return (
    <section className="panel" data-panel="policies">
      <div className="panel-header">
        <h2><FileText size={14} /> Share policies</h2>
      </div>
      <div className="share-form">
        <select className="share-input" value={template} onChange={(e) => setTemplate(e.target.value)} aria-label="Template to install">
          <option value="">install a template…</option>
          {templates.data?.data.map((t) => (
            <option key={t.name} value={t.name}>{t.name}</option>
          ))}
        </select>
        <button className="btn btn-ghost btn-xs" type="button" disabled={!template || install.isPending} onClick={() => install.mutate(template)}>
          Install
        </button>
      </div>
      {install.error && <p className="error-msg">{errorText(install.error)}</p>}
      {policies.error && <p className="error-msg">{errorText(policies.error)}</p>}
      {policies.data?.data.length === 0 && <p className="empty">No policies yet: install a template or write one below.</p>}
      <ul className="share-list">
        {policies.data?.data.map((p) => (
          <li key={p.id} className="share-row" data-policy={p.name}>
            <div className="share-row-head">
              <span className="share-name">{p.name}</span>
              <span className="badge badge-completed">v{p.version}</span>
              <span className="muted">{fmtDate(p.updated_at)}</span>
              <button className="btn btn-ghost btn-xs" type="button" onClick={() => startEdit(p)}>Edit</button>
            </div>
          </li>
        ))}
      </ul>
      <form
        className="share-form share-form--stack"
        onSubmit={(e) => {
          e.preventDefault();
          if (document.trim()) save.mutate();
        }}
      >
        <textarea
          className="share-textarea"
          rows={10}
          placeholder={'name: my-policy\ndata: "type==contact"\nexclude_classes: [personal]\nfields:\n  contact: [title, org, role]'}
          value={document}
          onChange={(e) => setDocument(e.target.value)}
          aria-label={editing ? `Edit policy ${editing.name}` : 'New policy document, YAML or JSON'}
        />
        <div className="share-form">
          <button className="btn btn-primary btn-xs" type="submit" disabled={save.isPending || !document.trim()}>
            {editing ? `Save ${editing.name} as v${editing.version + 1}` : 'Create policy'}
          </button>
          {editing && (
            <button className="btn btn-ghost btn-xs" type="button" onClick={() => { setEditing(null); setDocument(''); }}>
              Cancel
            </button>
          )}
        </div>
      </form>
      {save.error && <p className="error-msg">{errorText(save.error)}</p>}
    </section>
  );
}

function GrantsPanel() {
  const qc = useQueryClient();
  const grants = useQuery({ queryKey: ['share', 'grants'], queryFn: () => sharing.grants() });
  const audiences = useQuery({ queryKey: ['share', 'audiences'], queryFn: () => sharing.audiences() });
  const policies = useQuery({ queryKey: ['share', 'policies'], queryFn: () => sharing.policies() });
  const [audience, setAudience] = useState('');
  const [policy, setPolicy] = useState('');
  const [days, setDays] = useState('');
  const invalidate = () => qc.invalidateQueries({ queryKey: ['share'] });

  const grant = useMutation({
    mutationFn: () => sharing.createGrant(audience, policy, expiryFromDays(days)),
    onSuccess: () => { setDays(''); void invalidate(); },
  });
  const revoke = useMutation({ mutationFn: (id: string) => sharing.revokeGrant(id), onSuccess: invalidate });

  return (
    <section className="panel" data-panel="grants">
      <div className="panel-header">
        <h2><KeyRound size={14} /> Grants</h2>
      </div>
      <form
        className="share-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (audience && policy) grant.mutate();
        }}
      >
        <select className="share-input" value={audience} onChange={(e) => setAudience(e.target.value)} aria-label="Audience">
          <option value="">audience…</option>
          {audiences.data?.data.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
        <select className="share-input" value={policy} onChange={(e) => setPolicy(e.target.value)} aria-label="Policy">
          <option value="">policy…</option>
          {policies.data?.data.map((p) => <option key={p.id} value={p.id}>{p.name} v{p.version}</option>)}
        </select>
        <input
          className="share-input share-input--short"
          placeholder="days"
          inputMode="numeric"
          value={days}
          onChange={(e) => setDays(e.target.value)}
          aria-label="Grant expiry in days"
        />
        <button className="btn btn-primary btn-xs" type="submit" disabled={grant.isPending || !audience || !policy}>
          Grant
        </button>
      </form>
      {grant.error && <p className="error-msg">{errorText(grant.error)}</p>}
      {grants.error && <p className="error-msg">{errorText(grants.error)}</p>}
      {grants.data?.data.length === 0 && <p className="empty">No grants: nobody sees anything yet.</p>}
      <ul className="share-list">
        {grants.data?.data.map((g: ShareGrant) => (
          <li key={g.id} className="share-row share-row--inline" data-grant={g.id}>
            <Share2 size={12} className="muted" />
            <span className="share-name">{g.audience}</span>
            <span className="muted">sees</span>
            <span className="share-name">{g.policy}</span>
            <span className="badge badge-completed">v{g.policy_version}</span>
            <span className="muted">{g.expires_at ? `until ${fmtDate(g.expires_at)}` : 'no expiry'}</span>
            <button className="btn btn-ghost btn-xs" type="button" disabled={revoke.isPending} onClick={() => revoke.mutate(g.id)}>
              Revoke
            </button>
          </li>
        ))}
      </ul>
      {revoke.error && <p className="error-msg">{errorText(revoke.error)}</p>}
    </section>
  );
}
