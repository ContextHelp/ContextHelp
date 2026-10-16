# Workflow: Share Your Network with Colleagues and Partners

## Goal

Share part of what your dpkms instance holds with people you invite, from your own instance, without an external identity provider. The first case this is built for: a contact network shared with work colleagues without phone numbers or email addresses, and with partners who get the contact details but never your personal notes.

Three things decide who sees what:

- An **audience** is a named group of principals, such as `colleagues` or `partners`. A person can be in several, and a membership can expire.
- A **share policy** is a named, versioned YAML document: the data filter objects must match, the content classes it excludes, the fields it renders per type and the capabilities it opens.
- A **grant** gives an audience a policy, with an optional expiry. Grants are the only thing that gives a non-owner any data.

The owner sees everything and pays nothing for it. Everyone else sees nothing until a grant applies to them.

## Prerequisites

- A protected or public instance (`server.access`), with `server.public_url` set so invite links point somewhere people can open.
- ctxt configured with the owner's API key: every `ctxt share` command calls the instance over the API and needs `admin:shares`, which only the `admin` role holds.
- Contacts ingested after the contact ingest change: email, phone and address live in metadata, never in the text, and the vCard note is a separate part with the `personal` class. Contacts stored before it are re-ingested.

## Quick start: the contact case

Install the two built-in policies, create the two audiences, grant each one its policy, then invite one person into each.

```bash
ctxt share template install colleagues-network
ctxt share template install partners-network
ctxt share audience create colleagues
ctxt share audience create partners
ctxt share grant colleagues colleagues-network
ctxt share grant partners partners-network --expires 90d
ctxt share invite --name "Sam Colleague" --audience colleagues
ctxt share invite --name "Dana Partner" --audience partners
```

Each invite prints a single-use link. Whoever opens it becomes that person, lands in the audience named by `--audience`, and the audience's grants apply from their first request. `ctxt share audience list` shows who is in what; the web UI shows the same under **Sharing**.

## Audiences

```bash
ctxt share audience create partners
ctxt share audience add partners prn_01J... --expires 90d
ctxt share audience remove partners prn_01J...
ctxt share audience list
```

A name is a lowercase slug, unique on the instance. Adding a principal that is already a member replaces its expiry. The owner joins no audience, and a suspended or revoked principal is refused. `list` prints every audience with its members and their expiry; `--format json` carries every field, grant ids included.

Members and grants can both expire, and so can invites and keys. Whichever ends first ends the view.

## Share policies

A policy is the YAML document of ADR-080. This is the built-in `colleagues-network`:

```yaml
name: colleagues-network
data: "type==contact;tag=in=(work,client)"   # RSQL; what an object must match
exclude_classes: [personal]                   # objects and parts with these classes are gone
fields:
  contact: [title, org, role, tags, city]     # rendered fields, per type
  "*": [title, summary, tags, created_at]     # every other visible type
redact:
  mask: [email, phone, address]               # kept for the masking phase; inert today
  style: category
capabilities:
  search: [fts, vector]                       # graph | fts | vector | rsql
  entities: true
  export: false
  mcp: true
  events: false
```

```bash
ctxt share template list
ctxt share template install partners-network
ctxt share policy create colleagues.yaml        # or pipe YAML on stdin
ctxt share policy show colleagues-network       # YAML, with id and version in a comment
ctxt share policy edit colleagues-network colleagues.yaml
ctxt share policy list
```

The instance validates every document and refuses one that does not parse, naming the field: `data: field "mention" reaches across objects and is refused in a policy`. The data filter may use `type`, `subtype`, `tag`, `profile`, `class`, `source`, the date fields and `metadata.<key>`; `mention` and `related` are refused because they reach other objects. Every edit bumps the version and the document keeps its name: a policy is never renamed through an edit. A template installs once; a second install is refused so an edited policy is never reset.

`policy show` prints a document `policy edit` accepts as is, which makes a sed pipeline the quickest edit.

## Grants

```bash
ctxt share grant partners partners-network --expires 30d
ctxt share revoke grt_01J...
```

A grant binds one audience to one policy. An object is visible to a viewer when any of its grants matches it and that same grant's exclusions do not remove it; the viewer sees the union of the fields the matching grants allow. Exclusions are per grant: `exclude_classes` in one grant does not hide what another grant shows. A revoked grant stops applying at once and stays on record.

## The ceiling

```bash
ctxt share ceiling prn_01J... --deny-classes personal
ctxt share ceiling prn_01J... --deny-classes ""
```

A ceiling lists the content classes a principal may never see, whatever its grants say. It is the answer to a partner who is also a colleague: both views apply, and `personal` still never does. An empty value clears it.

## Keys narrow, never widen

An API key can be bound to a share policy. The key then sees the intersection of its principal's view and that policy, which is how an assistant's key stays narrower than the person it acts for. A principal binds its own keys through `PUT /api/v1/self/keys/{key}/policy` with `{"policy": "<name>"}`; the owner binds anyone's through `PUT /api/v1/share/keys/{key}/policy`. An empty policy unbinds.

## Invite into an audience

```bash
ctxt share invite --name "Dana Partner" --audience partners
```

The link works once and expires after 7 days unless `--expires` says otherwise. Accepting it creates the person, puts them in the audience (added by you, with no expiry) and signs their browser in. An invite minted without `--audience` creates a person who sees nothing until you add them somewhere. If the audience is gone by the time the link is opened, the link is spent and nobody is created: mint a new one.

## The web UI

An admin's browser session holds `admin:shares`, so the owner sees a **Sharing** page with the same operations: audiences with their members, policies with their version, grants with their expiry, and a template to install. The page is hidden from everyone else.

A shared viewer sees a banner above every page, `shared by <owner> · <policy>`, naming who shares with them and the policies in force. It comes from `GET /api/v1/whoami`, which reports `shared` for a non-owner principal that at least one grant applies to.

## Preview what a viewer sees

```bash
ctxt share preview --as colleagues-network show <object-id>
ctxt share preview --as prn_01J... find "sarah"
ctxt --as partners-network list --type contact
```

The owner can run any read command through another viewer's view: a principal id or a share policy name. The instance answers exactly what that viewer would get, through the same projection, the same checks and the same audit, so you can check a policy before the invite goes out. `ctxt share preview` runs one read command; `--as` on any read command does the same. Only the owner may preview, and only routes that read: a write sent with `--as` is refused. A policy name previews the policy on its own, as if it were the viewer's only grant; a principal id previews that person's full view, grants, ceiling and all. A key's own narrowing cannot be previewed this way, since a key is not a principal; preview the policy the key is bound to instead. Every preview is written to the read audit as your request, with the viewer named.

## Audit who read what

```bash
ctxt share audit --object <object-id>
ctxt share audit --principal prn_01J... --since 7d
```

Every request by a non-owner principal leaves one entry: who, through which credential, on which route, under which policy versions, with the objects the answer carried. `--object` asks who saw one object; `--principal` asks what one viewer read; `--since` takes a date, an RFC 3339 time or a duration such as `7d`. Your own requests are not logged, except previews. The API behind it is `GET /api/v1/share/audit`, under `admin:audit`.

Entries are buffered and written in batches by one goroutine, so a read never waits on the writer. Entries still in the buffer when the process dies are lost; everything written before them is safe. Object ids are kept for `sharing.audit.retention` (`2160h`, 90 days, by default) and pruned by `dpkms housekeeping run`; the entries and their counts stay.

## The backstop

Before an answer leaves the instance, a last check walks the known id paths of the route's declared response shape and searches the body for the values the render layer withheld. Anything the request did not release is stripped, logged as `security.visibility_backstop` and counted. With `dpkms serve --strict-sharing` the request answers 500 instead. This is how every test instance runs: a hit is a bug in the scoped reads or the render layer, never normal operation. The check runs only for shared viewers; the owner pays nothing.

## What a viewer can search

Shared contacts are rendered from their visible fields, so a colleague's card reads `Sarah Lee | Head of Ops @ Acme` and carries no email, phone or note. Two limits apply in this version:

- A partner sees the email on the card but cannot search by it. Searching an email address returns nothing for anyone until the revealed-category index lands with the masking phase. A colleague cannot match on any field their policy leaves out.
- Hiding a field inside the searchable minimum does not hide its terms from search. Each structured type declares the fields that go into the shared search index; for contacts those are `title`, `org`, `role`, `tags` and `city`. A policy that projects out `org` still lets a viewer match on a company name, though the card will not show it. A field that must not even match goes behind a class, with `exclude_classes`, not behind projection.

## Current runtime

This version ships the whole contact slice: audiences, policies, grants, ceilings, key bindings, invites, the Sharing page and the banner; the per-viewer read path that filters objects, edges, entities and search by grants; the render layer that rebuilds a contact from its visible fields and drops derived fields (a contact's summary derives from the whole card, so a viewer never sees one); the backstop; `ctxt share preview` and `ctxt share audit`. Span masking of free text, the `redact` block, is still inert.

## Output validation checklist

- `ctxt share audience list` names every invited person under the audience you expected, with the expiry you gave.
- `ctxt share policy list` shows the version you expect after each edit.
- `ctxt share audience list --format json` shows one active grant per audience and policy.
- A shared viewer's `GET /api/v1/whoami` carries `shared.policies` naming the policies you granted.
- `ctxt share preview --as <policy> show <object-id>` renders the card without the fields the policy leaves out.
- `ctxt share audit --principal <id>` lists one entry per request the viewer made, with the objects it carried.

## Troubleshooting

### `no such audience`

The name or id does not exist on the instance. `ctxt share audience list` shows what does; names are lowercase slugs.

### The document is refused

The error names the field: `data:` for the filter, `fields:` for a type's list, `capabilities.search:` for a mode. `mention` and `related` are never accepted in a policy. A `version:` key in the document is ignored; the instance owns it.

### `policy already exists`

A template or a document with a name already stored. Edit the existing policy, or create your own under another name from `ctxt share policy show`.

### 403 `INSUFFICIENT_SCOPE`

The key or session lacks `admin:shares`. Only the `admin` role holds it, and only an admin's browser session does. A person's own key binding goes through `/self/keys/{key}/policy` under `write:self` instead.

## Story alignment

This covers the contact-sharing slice of ADR-080: audiences, share policies and grants (§1, §2), the per-principal ceiling and the owner's preview (§2), the render layer (§5), the backstop (§8), the read audit (§9), invites into audiences and the management surface (§11).
