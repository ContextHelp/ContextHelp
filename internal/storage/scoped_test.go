package storage

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fakeViewer is a Viewer whose answer is its value.
type fakeViewer bool

func (v fakeViewer) IsUnrestricted() bool { return bool(v) }

// nilDriver is a StorageDriver whose every accessor returns a nil store,
// so a wrapper method that reaches the store panics on the nil
// interface and one that refuses first does not. That difference is how
// the tests tell a read that fails closed from a write that passes
// through, without a recording double per store.
type nilDriver struct{}

func (nilDriver) Init(context.Context) error          { return nil }
func (nilDriver) Close(context.Context) error         { return nil }
func (nilDriver) Health(context.Context) error        { return nil }
func (nilDriver) Objects() ObjectStore                { return nil }
func (nilDriver) Entities() EntityStore               { return nil }
func (nilDriver) Edges() EdgeStore                    { return nil }
func (nilDriver) Jobs() JobStore                      { return nil }
func (nilDriver) Pipelines() PipelineStore            { return nil }
func (nilDriver) Steps() StepStore                    { return nil }
func (nilDriver) Registries() RegistryStore           { return nil }
func (nilDriver) Reminders() ReminderStore            { return nil }
func (nilDriver) Feeds() FeedStore                    { return nil }
func (nilDriver) FeedItems() FeedItemStore            { return nil }
func (nilDriver) Batches() BatchStore                 { return nil }
func (nilDriver) Detectors() DetectorStore            { return nil }
func (nilDriver) Blobs() BlobStore                    { return nil }
func (nilDriver) Proximity() ProximityStore           { return nil }
func (nilDriver) Watches() WatchStore                 { return nil }
func (nilDriver) Aliases() AliasStore                 { return nil }
func (nilDriver) AuditLog() AuditStore                { return nil }
func (nilDriver) Attachments() AttachmentStore        { return nil }
func (nilDriver) Resurfacing() ResurfacingQueueStore  { return nil }
func (nilDriver) Entitlements() EntitlementStore      { return nil }
func (nilDriver) PendingEntities() PendingEntityStore { return nil }
func (nilDriver) Metering() MeteringStore             { return nil }
func (nilDriver) Embeddings() EmbeddingStore          { return nil }
func (nilDriver) SavedSearches() SavedSearchStore     { return nil }
func (nilDriver) SearchHistory() SearchHistoryStore   { return nil }
func (nilDriver) Watermarks() WatermarkStore          { return nil }
func (nilDriver) UISessions() UISessionStore          { return nil }
func (nilDriver) Auth() AuthStore                     { return nil }
func (nilDriver) Sharing() SharingStore               { return nil }

var driverType = reflect.TypeOf((*StorageDriver)(nil)).Elem()

func TestScopedDriverUnrestrictedIsTheDriver(t *testing.T) {
	d := &mockDriver{}
	if got := ScopedDriver(fakeViewer(true), d); got != StorageDriver(d) {
		t.Fatalf("ScopedDriver(unrestricted) = %T, want the driver itself", got)
	}
	if ScopedDriver(fakeViewer(true), nil) != nil {
		t.Fatal("ScopedDriver(_, nil) must stay nil")
	}
}

func TestScopedDriverRestrictsEveryOtherViewer(t *testing.T) {
	d := &mockDriver{}
	for name, v := range map[string]Viewer{"restricted": fakeViewer(false), "nil viewer": nil} {
		got := ScopedDriver(v, d)
		if got == StorageDriver(d) {
			t.Fatalf("%s: ScopedDriver returned the raw driver", name)
		}
		if _, err := got.Objects().Get(context.Background(), "x"); !errors.Is(err, ErrRestrictedViewer) {
			t.Fatalf("%s: Objects().Get err = %v, want ErrRestrictedViewer", name, err)
		}
	}
}

// Every method of StorageDriver is classified, and every scoped
// accessor is overridden on scopedDriver rather than promoted from the
// embedded driver: a new accessor on the interface fails here until it
// is classified and, when it returns a store, wrapped.
func TestScopedAccessorsCoverDriver(t *testing.T) {
	scoped := ScopedDriver(fakeViewer(false), &mockDriver{})
	sv := reflect.ValueOf(scoped)
	for i := 0; i < driverType.NumMethod(); i++ {
		m := driverType.Method(i)
		kind, ok := scopedAccessors[m.Name]
		if !ok {
			t.Errorf("StorageDriver.%s is not classified in scopedAccessors: add it as accessorScoped (and wrap it on scopedDriver) or accessorPassthrough", m.Name)
			continue
		}
		if kind != accessorScoped {
			continue
		}
		if m.Type.NumIn() != 0 || m.Type.NumOut() != 1 {
			t.Errorf("scoped accessor %s must take nothing and return one store", m.Name)
			continue
		}
		out := sv.MethodByName(m.Name).Call(nil)[0]
		if out.IsNil() {
			t.Errorf("scopedDriver.%s returned nil: promoted from the embedded driver instead of wrapped", m.Name)
			continue
		}
		if name := out.Elem().Type().Name(); !strings.HasPrefix(name, "restricted") {
			t.Errorf("scopedDriver.%s returned %s, want a restricted wrapper", m.Name, out.Elem().Type())
		}
		if _, ok := restrictedReads[m.Name]; !ok {
			t.Errorf("scoped accessor %s has no entry in restrictedReads", m.Name)
		}
	}
	for name := range scopedAccessors {
		if _, ok := driverType.MethodByName(name); !ok {
			t.Errorf("scopedAccessors names %s, which StorageDriver no longer has", name)
		}
	}
}

// Every listed read names a method of its store, and no store method is
// variadic (the reflection-driven checks call them with zero arguments).
func TestRestrictedReadsNameStoreMethods(t *testing.T) {
	for accessor, reads := range restrictedReads {
		m, ok := driverType.MethodByName(accessor)
		if !ok {
			t.Errorf("restrictedReads names accessor %s, which StorageDriver does not have", accessor)
			continue
		}
		store := m.Type.Out(0)
		for _, r := range reads {
			if _, ok := store.MethodByName(r); !ok {
				t.Errorf("restrictedReads: %s.%s is not a method of %s", accessor, r, store.Name())
			}
		}
		for i := 0; i < store.NumMethod(); i++ {
			if store.Method(i).Type.IsVariadic() {
				t.Errorf("%s.%s is variadic; the scoped checks do not call variadic methods", store.Name(), store.Method(i).Name)
			}
		}
	}
}

// zeroArgs builds one argument per parameter of m: a background context
// for context parameters, the zero value otherwise.
func zeroArgs(m reflect.Method) []reflect.Value {
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	args := make([]reflect.Value, 0, m.Type.NumIn())
	for i := 0; i < m.Type.NumIn(); i++ {
		in := m.Type.In(i)
		if in == ctxType {
			args = append(args, reflect.ValueOf(context.Background()))
			continue
		}
		args = append(args, reflect.Zero(in))
	}
	return args
}

// callStore invokes method on store and reports whether it panicked on
// the way (the nil inner store was reached) and the error it returned.
func callStore(store reflect.Value, m reflect.Method) (reached bool, err error) {
	defer func() {
		if recover() != nil {
			reached = true
		}
	}()
	out := store.MethodByName(m.Name).Call(zeroArgs(m))
	if len(out) == 0 {
		return false, nil
	}
	last := out[len(out)-1]
	if e, ok := last.Interface().(error); ok {
		return false, e
	}
	return false, nil
}

// For a restricted viewer every listed read fails closed without
// touching the store, with zero results beside the error, and every
// other method reaches the store.
func TestRestrictedStoresFailClosedOnReadsAndPassWrites(t *testing.T) {
	scoped := reflect.ValueOf(ScopedDriver(fakeViewer(false), nilDriver{}))
	for accessor, reads := range restrictedReads {
		store := scoped.MethodByName(accessor).Call(nil)[0]
		// The accessor's declared return type: the store interface,
		// whose method signatures carry no receiver.
		storeType := store.Type()
		for i := 0; i < storeType.NumMethod(); i++ {
			m := storeType.Method(i)
			isRead := slices.Contains(reads, m.Name)
			reached, err := callStore(store, m)
			switch {
			case isRead && reached:
				t.Errorf("%s.%s: listed as a read but reached the store", accessor, m.Name)
			case isRead && !errors.Is(err, ErrRestrictedViewer):
				t.Errorf("%s.%s: read returned %v, want ErrRestrictedViewer", accessor, m.Name, err)
			case isRead:
				assertZeroResults(t, accessor, store, m)
			case !reached:
				t.Errorf("%s.%s: not listed as a read, yet never reached the store (err=%v)", accessor, m.Name, err)
			}
		}
	}
}

// assertZeroResults checks a refused read returns nothing beside its
// error.
func assertZeroResults(t *testing.T, accessor string, store reflect.Value, m reflect.Method) {
	t.Helper()
	out := store.MethodByName(m.Name).Call(zeroArgs(m))
	for i, v := range out[:len(out)-1] {
		if !v.IsZero() {
			t.Errorf("%s.%s: result %d = %v, want zero on a refused read", accessor, m.Name, i, v.Interface())
		}
	}
}

func TestRestrictedReadsExportedCopy(t *testing.T) {
	got := RestrictedReads()
	if !reflect.DeepEqual(got, restrictedReads) {
		t.Fatal("RestrictedReads() differs from the table")
	}
	got["Objects"][0] = "mutated"
	if restrictedReads["Objects"][0] == "mutated" {
		t.Fatal("RestrictedReads() shares its slices with the table")
	}
}

func TestRestrictedReadNamesStoreAndMethod(t *testing.T) {
	err := restrictedRead("Objects", "Get")
	if !errors.Is(err, ErrRestrictedViewer) || !strings.HasPrefix(err.Error(), "Objects.Get: ") {
		t.Fatalf("restrictedRead = %v", err)
	}
}
