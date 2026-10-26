package sqlstream

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const testDriverName = "dbl_sqlstream_test"

var registerTestDriver sync.Once

type testDriver struct{}
type testConn struct{}
type testRows struct {
	values [][]driver.Value
	index  int
}

var (
	testRowsMu sync.Mutex
	testData   [][]driver.Value
)

func registerDriver() {
	registerTestDriver.Do(func() { sql.Register(testDriverName, testDriver{}) })
}

func (testDriver) Open(string) (driver.Conn, error)  { return testConn{}, nil }
func (testConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (testConn) Close() error                        { return nil }
func (testConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (testConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	testRowsMu.Lock()
	defer testRowsMu.Unlock()
	values := make([][]driver.Value, len(testData))
	for i, row := range testData {
		values[i] = append([]driver.Value(nil), row...)
	}
	return &testRows{values: values}, nil
}

func (*testRows) Columns() []string { return []string{"id", "payload"} }
func (*testRows) Close() error      { return nil }

func (rows *testRows) Next(dest []driver.Value) error {
	if rows.index == len(rows.values) {
		return io.EOF
	}
	copy(dest, rows.values[rows.index])
	rows.index++
	return nil
}

func openTestDB(t *testing.T, data [][]driver.Value) *sql.DB {
	t.Helper()
	registerDriver()
	testRowsMu.Lock()
	testData = data
	testRowsMu.Unlock()
	db, err := sql.Open(testDriverName, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestQueryStreamsBoundedBatches(t *testing.T) {
	db := openTestDB(t, [][]driver.Value{
		{int64(1), "one"},
		{int64(2), "two"},
		{int64(3), "three"},
	})
	var events []Event
	for event := range Query(context.Background(), db, "select", 2) {
		events = append(events, event)
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want columns + 2 batches + completion", len(events))
	}
	if got := len(events[0].Columns); got != 2 {
		t.Fatalf("got %d columns, want 2", got)
	}
	if got := len(events[1].Rows); got != 2 {
		t.Fatalf("first batch has %d rows, want 2", got)
	}
	if got := len(events[2].Rows); got != 1 {
		t.Fatalf("second batch has %d rows, want 1", got)
	}
	if !events[3].Done || events[3].RowsRead != 3 || events[3].Err != nil {
		t.Fatalf("unexpected completion event: %+v", events[3])
	}
}

func TestCursorReadsOnlyRequestedBatches(t *testing.T) {
	db := openTestDB(t, [][]driver.Value{
		{int64(1), "one"},
		{int64(2), "two"},
		{int64(3), "three"},
	})
	cursor, err := Open(context.Background(), db, "select")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cursor.Close() })
	if got := len(cursor.Columns()); got != 2 {
		t.Fatalf("got %d columns, want 2", got)
	}
	first, done, err := cursor.NextBatch(2)
	if err != nil || done || len(first) != 2 {
		t.Fatalf("first page = %d rows, done=%v, err=%v", len(first), done, err)
	}
	if got := cursor.RowsRead(); got != 2 {
		t.Fatalf("after first page read %d rows, want 2", got)
	}
	last, done, err := cursor.NextBatch(2)
	if err != nil || !done || len(last) != 1 {
		t.Fatalf("last page = %d rows, done=%v, err=%v", len(last), done, err)
	}
}

func TestCursorFinishesAdapterHookOnCompletionAndEarlyClose(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		db := openTestDB(t, [][]driver.Value{{int64(1), "one"}})
		cursor, err := Open(context.Background(), db, "select")
		if err != nil {
			t.Fatal(err)
		}
		var completed *bool
		cursor.OnFinish(func(value bool) error {
			completed = &value
			return nil
		})
		if _, done, err := cursor.NextBatch(2); err != nil || !done {
			t.Fatalf("NextBatch done=%v err=%v, want completed", done, err)
		}
		if completed == nil || !*completed {
			t.Fatalf("completion hook value = %v, want true", completed)
		}
	})

	t.Run("early close", func(t *testing.T) {
		db := openTestDB(t, [][]driver.Value{{int64(1), "one"}, {int64(2), "two"}})
		cursor, err := Open(context.Background(), db, "select")
		if err != nil {
			t.Fatal(err)
		}
		var completed *bool
		cursor.OnFinish(func(value bool) error {
			completed = &value
			return nil
		})
		if _, done, err := cursor.NextBatch(1); err != nil || done {
			t.Fatalf("NextBatch done=%v err=%v, want an open cursor", done, err)
		}
		if err := cursor.Close(); err != nil {
			t.Fatal(err)
		}
		if completed == nil || *completed {
			t.Fatalf("completion hook value = %v, want false", completed)
		}
	})
}

func TestCursorAndStreamBoundBatchesByEstimatedBytes(t *testing.T) {
	largeValue := strings.Repeat("x", MaxBatchBytes/2)
	data := [][]driver.Value{{largeValue}, {largeValue}, {largeValue}}
	db := openTestDB(t, data)

	cursor, err := Open(context.Background(), db, "select")
	if err != nil {
		t.Fatal(err)
	}
	first, done, err := cursor.NextBatch(100)
	if err != nil || done || len(first) != 2 {
		t.Fatalf("cursor first batch = %d rows, done=%v, err=%v; want 2 rows", len(first), done, err)
	}
	last, done, err := cursor.NextBatch(100)
	if err != nil || !done || len(last) != 1 {
		t.Fatalf("cursor final batch = %d rows, done=%v, err=%v; want 1 row", len(last), done, err)
	}
	_ = cursor.Close()

	var batchRows []int
	for event := range Query(context.Background(), db, "select", 100) {
		if len(event.Rows) > 0 {
			batchRows = append(batchRows, len(event.Rows))
		}
	}
	if want := []int{2, 1}; !reflect.DeepEqual(batchRows, want) {
		t.Fatalf("stream batch row counts = %v, want %v", batchRows, want)
	}
}

func TestCursorStopsAndClosesRowsWhenContextIsCanceled(t *testing.T) {
	registerBlockingDriver()
	ctx, cancel := context.WithCancel(context.Background())
	signals := blockingSignals{entered: make(chan struct{}), closed: make(chan struct{})}
	ctx = context.WithValue(ctx, blockingSignalsKey{}, signals)
	db, err := sql.Open(blockingDriverName, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cursor, err := Open(ctx, db, "blocking")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan struct {
		rows [][]any
		done bool
		err  error
	}, 1)
	go func() {
		rows, done, err := cursor.NextBatch(1)
		result <- struct {
			rows [][]any
			done bool
			err  error
		}{rows: rows, done: done, err: err}
	}()
	select {
	case <-signals.entered:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("cursor did not start reading a row")
	}
	cancel()
	select {
	case got := <-result:
		if !got.done || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("NextBatch = (done %v, err %v), want canceled completion", got.done, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("NextBatch did not stop after context cancellation")
	}
	select {
	case <-signals.closed:
	case <-time.After(time.Second):
		t.Fatal("driver rows were not closed after context cancellation")
	}
}

func TestCursorCloseSerializesWithCanceledBatchRead(t *testing.T) {
	registerBlockingDriver()
	ctx, cancel := context.WithCancel(context.Background())
	signals := blockingSignals{entered: make(chan struct{}), closed: make(chan struct{})}
	ctx = context.WithValue(ctx, blockingSignalsKey{}, signals)
	db, err := sql.Open(blockingDriverName, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cursor, err := Open(ctx, db, "blocking")
	if err != nil {
		t.Fatal(err)
	}
	nextDone := make(chan error, 1)
	go func() {
		_, _, err := cursor.NextBatch(1)
		nextDone <- err
	}()
	select {
	case <-signals.entered:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("cursor did not start reading a row")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- cursor.Close() }()
	cancel()
	select {
	case err := <-nextDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("NextBatch error = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("NextBatch did not finish after cancellation")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("concurrent Close error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after NextBatch was canceled")
	}
	if err := cursor.Close(); err != nil {
		t.Fatalf("repeated Close error = %v", err)
	}
}

func TestDetachCopiesByteValues(t *testing.T) {
	source := []byte("first")
	result := detach(source).([]byte)
	source[0] = 'x'
	if got := string(result); got != "first" {
		t.Fatalf("detached bytes changed to %q", got)
	}
}

func TestWindowBoundsMemoryAndTracksVisibleRange(t *testing.T) {
	window := Window{MaxRows: 3}
	window.Append([][]any{{1}, {2}})
	window.Append([][]any{{3}, {4}, {5}})
	if got := window.Len(); got != 3 {
		t.Fatalf("retained %d rows, want bounded window of 3", got)
	}
	if got := window.FirstRow; got != 2 {
		t.Fatalf("first retained row offset is %d, want 2", got)
	}
	if got := window.Total; got != 5 {
		t.Fatalf("total rows read is %d, want 5", got)
	}
	if got := window.Row(0)[0]; got != 3 {
		t.Fatalf("first retained row is %v, want row 3", got)
	}
}

func TestWindowRingRetainsNewestRowsAcrossMultipleWraps(t *testing.T) {
	window := Window{MaxRows: 3}
	window.Append([][]any{{1}, {2}, {3}, {4}, {5}, {6}, {7}})
	if got := window.FirstRow; got != 4 {
		t.Fatalf("first retained row offset is %d, want 4", got)
	}
	if got := window.Total; got != 7 {
		t.Fatalf("total rows read is %d, want 7", got)
	}
	for i, want := range []int{5, 6, 7} {
		if got := window.Row(i)[0]; got != want {
			t.Fatalf("retained row %d = %v, want %d", i, got, want)
		}
	}
}

func TestWindowByteBudgetEvictsOldRowsAndReportsOversizedRows(t *testing.T) {
	rowSize := estimateRowBytes([]any{strings.Repeat("x", 24)})
	window := Window{MaxRows: 10, MaxBytes: rowSize*2 - 1}
	window.Append([][]any{{strings.Repeat("a", 24)}, {strings.Repeat("b", 24)}})
	if window.Len() != 1 || window.FirstRow != 1 || window.Total != 2 {
		t.Fatalf("after byte eviction: len=%d first=%d total=%d, want 1/1/2", window.Len(), window.FirstRow, window.Total)
	}
	if got := window.Row(0)[0]; got != strings.Repeat("b", 24) {
		t.Fatalf("retained row = %v, want newest row", got)
	}
	if window.bytes > window.MaxBytes {
		t.Fatalf("retained size = %d, exceeds byte budget %d", window.bytes, window.MaxBytes)
	}

	window.Append([][]any{{strings.Repeat("z", int(window.MaxBytes))}})
	if window.Len() != 1 || window.FirstRow != 1 || window.Total != 3 || window.OmittedRows != 1 {
		t.Fatalf("after oversized row: len=%d first=%d total=%d omitted=%d, want 1/1/3/1", window.Len(), window.FirstRow, window.Total, window.OmittedRows)
	}
	window.Append([][]any{{"tail"}})
	if window.Len() != 2 || window.FirstRow != 1 || window.Total != 4 || window.Row(0)[0] != strings.Repeat("b", 24) || window.Row(1)[0] != "tail" {
		t.Fatalf("after row following omission: len=%d first=%d total=%d rows=%v/%v", window.Len(), window.FirstRow, window.Total, window.Row(0), window.Row(1))
	}
	if window.SourceRow(0) != 1 || window.SourceRow(1) != 3 {
		t.Fatalf("source rows = %d/%d, want 1/3", window.SourceRow(0), window.SourceRow(1))
	}
	window.MaxRows = 1
	window.Append([][]any{{"last"}})
	if window.Len() != 1 || window.FirstRow != 4 || window.SourceRow(0) != 4 || window.Row(0)[0] != "last" {
		t.Fatalf("after resizing across omitted source row: first=%d source=%d row=%v", window.FirstRow, window.SourceRow(0), window.Row(0))
	}
}

func TestWindowAppliesReducedByteBudgetAndKeepsSourceIndices(t *testing.T) {
	value := strings.Repeat("x", 10)
	rowSize := estimateRowBytes([]any{value})
	window := Window{MaxRows: 5, MaxBytes: rowSize * 8}
	window.Append([][]any{{"row-0-123"}, {"row-1-123"}, {"row-2-123"}})
	window.MaxBytes = rowSize * 2
	window.Append([][]any{{"row-3-123"}})
	if window.Len() != 2 || window.bytes > window.MaxBytes {
		t.Fatalf("after reducing byte budget: len=%d bytes=%d limit=%d", window.Len(), window.bytes, window.MaxBytes)
	}
	if window.SourceRow(0) != 2 || window.SourceRow(1) != 3 {
		t.Fatalf("retained source indices = %d/%d, want 2/3", window.SourceRow(0), window.SourceRow(1))
	}
	if window.Row(0)[0] != "row-2-123" || window.Row(1)[0] != "row-3-123" {
		t.Fatalf("retained rows = %v/%v, want rows 2 and 3", window.Row(0), window.Row(1))
	}
}

func TestWindowRingMatchesBoundedReferenceAcrossBatchSizes(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	window := Window{MaxRows: 17}
	var reference []int
	var total int64
	for step := 0; step < 500; step++ {
		if step%31 == 0 {
			window.MaxRows = random.Intn(24) + 1
		}
		batchSize := random.Intn(48) + 1
		batch := make([][]any, batchSize)
		for i := range batch {
			value := step*100 + i
			batch[i] = []any{value}
			reference = append(reference, value)
		}
		total += int64(batchSize)
		window.Append(batch)
		if excess := len(reference) - window.MaxRows; excess > 0 {
			reference = reference[excess:]
		}
		wantFirst := total - int64(len(reference))
		if window.FirstRow != wantFirst {
			t.Fatalf("step %d first row = %d, want %d", step, window.FirstRow, wantFirst)
		}
		if window.Total != total || window.Len() != len(reference) {
			t.Fatalf("step %d total/length = %d/%d, want %d/%d", step, window.Total, window.Len(), total, len(reference))
		}
		for i, want := range reference {
			if got := window.Row(i)[0]; got != want {
				t.Fatalf("step %d row %d = %v, want %d", step, i, got, want)
			}
			if got := window.SourceRow(i); got != wantFirst+int64(i) {
				t.Fatalf("step %d source row %d = %d, want %d", step, i, got, wantFirst+int64(i))
			}
		}
	}
}

func TestSortWindowRowIndicesUsesLogicalRingOrder(t *testing.T) {
	window := Window{MaxRows: 3}
	window.Append([][]any{{int64(4)}, {int64(1)}, {int64(3)}, {int64(2)}})
	got := SortWindowRowIndices(&window, 0, false)
	if want := []int{0, 2, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted ring row indices = %v, want %v", got, want)
	}
	for i, want := range []int64{1, 2, 3} {
		if value := window.Row(got[i])[0]; value != want {
			t.Fatalf("sorted window row %d = %v, want %d", i, value, want)
		}
	}
}

func TestSortRowIndicesKeepsSourceRowsAndSortsTypedValues(t *testing.T) {
	rows := [][]any{{int64(10)}, {int64(2)}, {nil}, {float64(3.5)}}
	ascending := SortRowIndices(rows, 0, false)
	if want := []int{2, 1, 3, 0}; !reflect.DeepEqual(ascending, want) {
		t.Fatalf("ascending row indices = %v, want %v", ascending, want)
	}
	descending := SortRowIndices(rows, 0, true)
	if want := []int{0, 3, 1, 2}; !reflect.DeepEqual(descending, want) {
		t.Fatalf("descending row indices = %v, want %v", descending, want)
	}
	if got := rows[0][0]; got != int64(10) {
		t.Fatalf("sorting changed source rows, first value = %v", got)
	}
}

func TestSortRowIndicesOrdersMixedSQLiteStorageClasses(t *testing.T) {
	rows := [][]any{
		{"1970-01-01T00:00:00Z"},
		{time.Unix(0, 0).UTC()},
		{[]byte{0x00}},
	}
	got := SortRowIndices(rows, 0, false)
	if want := []int{1, 0, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed storage-class order = %v, want %v", got, want)
	}
}

func TestSortRowIndicesComparesMixedNumericValuesExactly(t *testing.T) {
	rows := [][]any{
		{int64(1<<53 + 1)},
		{float64(1 << 53)},
		{uint64(^uint64(0))},
		{math.Nextafter(math.Ldexp(1, 64), 0)},
		{int64(-1 << 63)},
		{-math.Ldexp(1, 63)},
	}
	got := SortRowIndices(rows, 0, false)
	if want := []int{4, 5, 1, 0, 3, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed numeric order = %v, want %v", got, want)
	}
}

func BenchmarkSortRowIndices10K(b *testing.B) {
	rows := make([][]any, 10_000)
	for i := range rows {
		rows[i] = []any{int64(len(rows) - i)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SortRowIndices(rows, 0, false)
	}
}

func BenchmarkSortRowIndices10KMixedNumeric(b *testing.B) {
	rows := make([][]any, 10_000)
	for i := range rows {
		if i%2 == 0 {
			rows[i] = []any{int64(len(rows) - i)}
		} else {
			rows[i] = []any{float64(len(rows) - i)}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SortRowIndices(rows, 0, false)
	}
}

func TestMixedIntegerFloatComparisonMatchesExactRationalOrder(t *testing.T) {
	random := rand.New(rand.NewSource(19))
	for i := 0; i < 2000; i++ {
		floating := math.Float64frombits(random.Uint64())
		if math.IsNaN(floating) || math.IsInf(floating, 0) {
			continue
		}
		signed := int64(random.Uint64())
		wantSigned := new(big.Rat).SetInt64(signed).Cmp(new(big.Rat).SetFloat64(floating))
		if got := compareSQLValues(signed, floating); got != wantSigned {
			t.Fatalf("compare int64(%d) and float64(%g) = %d, want %d", signed, floating, got, wantSigned)
		}
		if got := compareSQLValues(floating, signed); got != -wantSigned {
			t.Fatalf("reverse compare float64(%g) and int64(%d) = %d, want %d", floating, signed, got, -wantSigned)
		}

		unsigned := random.Uint64()
		wantUnsigned := new(big.Rat).SetUint64(unsigned).Cmp(new(big.Rat).SetFloat64(floating))
		if got := compareSQLValues(unsigned, floating); got != wantUnsigned {
			t.Fatalf("compare uint64(%d) and float64(%g) = %d, want %d", unsigned, floating, got, wantUnsigned)
		}
		if got := compareSQLValues(floating, unsigned); got != -wantUnsigned {
			t.Fatalf("reverse compare float64(%g) and uint64(%d) = %d, want %d", floating, unsigned, got, -wantUnsigned)
		}
	}
}

func TestQueryStopsWhenConsumerCancels(t *testing.T) {
	data := make([][]driver.Value, 20)
	for i := range data {
		data[i] = []driver.Value{int64(i), "payload"}
	}
	db := openTestDB(t, data)
	ctx, cancel := context.WithCancel(context.Background())
	events := Query(ctx, db, "select", 1)
	<-events // column descriptions
	cancel()

	closed := make(chan struct{})
	go func() {
		for range events {
		}
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("query stream did not stop after cancellation")
	}
}

const blockingDriverName = "dbl_sqlstream_blocking_test"

var registerBlockingTestDriver sync.Once

type blockingSignalsKey struct{}

type blockingSignals struct {
	entered chan struct{}
	closed  chan struct{}
}

type blockingDriver struct{}
type blockingConn struct{}
type blockingRows struct {
	ctx     context.Context
	signals blockingSignals
	close   sync.Once
}

func registerBlockingDriver() {
	registerBlockingTestDriver.Do(func() { sql.Register(blockingDriverName, blockingDriver{}) })
}

func (blockingDriver) Open(string) (driver.Conn, error)  { return blockingConn{}, nil }
func (blockingConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (blockingConn) Close() error                        { return nil }
func (blockingConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (blockingConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	signals, ok := ctx.Value(blockingSignalsKey{}).(blockingSignals)
	if !ok {
		return nil, errors.New("blocking signals missing from context")
	}
	return &blockingRows{ctx: ctx, signals: signals}, nil
}

func (*blockingRows) Columns() []string { return []string{"value"} }

func (rows *blockingRows) Close() error {
	rows.close.Do(func() { close(rows.signals.closed) })
	return nil
}

func (rows *blockingRows) Next([]driver.Value) error {
	close(rows.signals.entered)
	<-rows.ctx.Done()
	return rows.ctx.Err()
}
