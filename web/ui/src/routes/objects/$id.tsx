import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { objects as objectsApi } from '@/api';
import { Loader2, Trash2, ArrowLeft } from 'lucide-react';

export const Route = createFileRoute('/objects/$id')({ component: ObjectDetail });

function ObjectDetail() {
  const { id } = Route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const { data: obj, isLoading, error } = useQuery({
    queryKey: ['object', id],
    queryFn: () => objectsApi.get(id),
  });

  const deleteMut = useMutation({
    mutationFn: () => objectsApi.delete(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['objects'] });
      await navigate({ to: '/objects' });
    },
  });

  if (isLoading) {
    return (
      <div className="view view--center">
        <Loader2 size={24} className="spin" />
      </div>
    );
  }

  if (error || !obj) {
    return (
      <div className="view">
        <p className="error-msg">{(error as Error)?.message ?? 'Object not found'}</p>
        <Link to="/objects" className="btn btn-ghost">
          <ArrowLeft size={14} /> Back
        </Link>
      </div>
    );
  }

  return (
    <div className="view">
      <header className="view-header">
        <div className="header-row">
          <Link to="/objects" className="back-link">
            <ArrowLeft size={14} /> Objects
          </Link>
          <button
            className="btn btn-danger"
            onClick={() => {
              if (confirm('Delete this object?')) deleteMut.mutate();
            }}
            disabled={deleteMut.isPending}
          >
            <Trash2 size={14} />
            {deleteMut.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>
        <h1 className="obj-detail-title">{obj.type}</h1>
        <p className="view-subtitle font-mono">{obj.id}</p>
      </header>

      <div className="detail-grid">
        <MetaRow label="Source" value={obj.source} />
        <MetaRow label="Type" value={obj.type} />
        {obj.subtype && <MetaRow label="Subtype" value={obj.subtype} />}
        {obj.pipeline && <MetaRow label="Pipeline" value={obj.pipeline} />}
        <MetaRow label="Created" value={fmtFull(obj.created_at)} />
        <MetaRow label="Updated" value={fmtFull(obj.updated_at)} />
      </div>

      {obj.text_content && (
        <section className="panel content-panel">
          <div className="panel-header"><h2>Content</h2></div>
          <pre className="content-pre">{obj.text_content}</pre>
        </section>
      )}

      {obj.raw_content && !obj.text_content && (
        <section className="panel content-panel">
          <div className="panel-header"><h2>Raw</h2></div>
          <pre className="content-pre">{obj.raw_content.slice(0, 4000)}</pre>
        </section>
      )}
    </div>
  );
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="meta-row">
      <span className="meta-label">{label}</span>
      <span className="meta-value">{value}</span>
    </div>
  );
}

function fmtFull(s: string) {
  return new Date(s).toLocaleString(undefined, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}
