package sharing

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ObjectPlanOf returns the plan object id renders under for v over d,
// as a scoped read decides it: the attribution query a read runs, then
// ObjectPlan over the grants it says cover the object.
func ObjectPlanOf(ctx context.Context, v Viewer, d storage.StorageDriver, id string) (RenderPlan, error) {
	rd, ok := Scoped(WithViewer(ctx, v), d, nil).(renderDriver)
	if !ok {
		return v.Plan(), nil
	}
	a, err := rd.attribute(ctx, []string{id})
	if err != nil {
		return RenderPlan{}, err
	}
	return v.ObjectPlan(a.of(id)), nil
}
