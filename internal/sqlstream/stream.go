// Package sqlstream reads SQL result sets in bounded batches.
package sqlstream

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

const (
	DefaultBatchSize      = 128
	MaxBatchBytes         = 8 << 20
	MaxBatchSize          = 4096
	DefaultWindowMaxBytes = 64 << 20
)

// Column describes a result column without retaining driver-owned metadata.
type Column struct {
	Name         string
	DatabaseType string
}

// Event is one ordered part of a streamed query. The first event includes the
// column descriptions; following events contain row batches. The last event
// has Done set and carries the final row count and any iteration error.
type Event struct {
	Columns  []Column
	Rows     [][]any
	RowsRead int64
	Done     bool
	Err      error
}

// Window retains the newest rows within both row-count and estimated-byte
// limits while tracking their source position. The full result is never
// accumulated in memory.
type Window struct {
	MaxRows     int
	MaxBytes    int64
	FirstRow    int64
	Total       int64
	OmittedRows int64

	rows       [][]any
	rowSizes   []int64
	sourceRows []int64
	start      int
	count      int
	bytes      int64
}

func (window *Window) Append(batch [][]any) {
	if len(batch) == 0 {
		return
	}
	limit := window.MaxRows
	if limit <= 0 {
		limit = DefaultBatchSize
	}
	maxBytes := window.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultWindowMaxBytes
	}
	window.resize(limit, maxBytes)
	for _, row := range batch {
		sourceRow := window.Total
		window.Total++
		rowSize := estimateRowBytes(row)
		if rowSize > maxBytes {
			window.OmittedRows++
			if window.count == 0 {
				window.FirstRow = window.Total
			}
			continue
		}
		for window.count >= limit || window.bytes+rowSize > maxBytes {
			window.dropOldest()
		}
		if window.count == 0 {
			window.FirstRow = sourceRow
		}
		position := (window.start + window.count) % limit
		window.rows[position] = row
		window.rowSizes[position] = rowSize
		window.sourceRows[position] = sourceRow
		window.count++
		window.bytes += rowSize
	}
}

func (window *Window) resize(limit int, maxBytes int64) {
	if len(window.rows) != limit {
		rows := make([][]any, limit)
		rowSizes := make([]int64, limit)
		sourceRows := make([]int64, limit)
		keep := window.count
		if keep > limit {
			keep = limit
		}
		drop := window.count - keep
		for i := 0; i < keep; i++ {
			oldPosition := (window.start + drop + i) % len(window.rows)
			rows[i] = window.rows[oldPosition]
			rowSizes[i] = window.rowSizes[oldPosition]
			sourceRows[i] = window.sourceRows[oldPosition]
		}
		window.rows = rows
		window.rowSizes = rowSizes
		window.sourceRows = sourceRows
		window.start = 0
		window.count = keep
		window.bytes = 0
		for _, size := range rowSizes[:keep] {
			window.bytes += size
		}
		if keep > 0 {
			window.FirstRow = sourceRows[0]
		}
	}
	for window.bytes > maxBytes && window.count > 0 {
		window.dropOldest()
	}
	if window.count == 0 {
		window.FirstRow = window.Total
	}
}

func (window *Window) dropOldest() {
	if window.count == 0 {
		return
	}
	window.bytes -= window.rowSizes[window.start]
	window.rows[window.start] = nil
	window.rowSizes[window.start] = 0
	window.sourceRows[window.start] = 0
	window.start = (window.start + 1) % len(window.rows)
	window.count--
	if window.count > 0 {
		window.FirstRow = window.sourceRows[window.start]
	} else {
		window.FirstRow = window.Total
	}
}

func estimateRowBytes(row []any) int64 {
	size := int64(32 + len(row)*16)
	for _, value := range row {
		switch value := value.(type) {
		case string:
			size += int64(len(value))
		case []byte:
			size += int64(len(value))
		case time.Time:
			size += 24
		}
	}
	return size
}

// Len returns the number of rows currently retained in the bounded window.
func (window *Window) Len() int {
	if window == nil {
		return 0
	}
	return window.count
}

// Row returns a retained row by its logical source order within the window.
func (window *Window) Row(index int) []any {
	if window == nil || index < 0 || index >= window.count || len(window.rows) == 0 {
		return nil
	}
	return window.rows[(window.start+index)%len(window.rows)]
}

// SourceRow returns the zero-based source row index for a retained logical row.
func (window *Window) SourceRow(index int) int64 {
	if window == nil || index < 0 || index >= window.count || len(window.sourceRows) == 0 {
		return -1
	}
	return window.sourceRows[(window.start+index)%len(window.sourceRows)]
}

// Cursor retains the driver's row cursor while the caller asks for more
// batches. It keeps memory bounded and lets the UI display a first page before
// the remaining result has been read.
type Cursor struct {
	mu        sync.Mutex
	ctx       context.Context
	rows      *sql.Rows
	columns   []Column
	rowsRead  int64
	done      bool
	finalized bool
	onFinish  func(bool) error
}

type QueryerContext interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func Open(ctx context.Context, db QueryerContext, statement string) (*Cursor, error) {
	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	names, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return nil, err
	}
	columns := make([]Column, len(names))
	for i, name := range names {
		columns[i] = Column{Name: name}
	}
	if types, typeErr := rows.ColumnTypes(); typeErr == nil {
		for i := range columns {
			if i < len(types) {
				columns[i].DatabaseType = types[i].DatabaseTypeName()
			}
		}
	}
	return &Cursor{ctx: ctx, rows: rows, columns: columns}, nil
}

func (cursor *Cursor) Columns() []Column {
	if cursor == nil {
		return nil
	}
	cursor.mu.Lock()
	defer cursor.mu.Unlock()
	return append([]Column(nil), cursor.columns...)
}

// OnFinish installs a callback that receives whether the result set was read
// to completion. It is useful for adapters that keep a transaction open while
// streaming rows and need to commit or roll it back when the cursor closes.
func (cursor *Cursor) OnFinish(callback func(completed bool) error) {
	if cursor == nil {
		return
	}
	cursor.mu.Lock()
	defer cursor.mu.Unlock()
	if !cursor.finalized {
		cursor.onFinish = callback
	}
}

func (cursor *Cursor) finish(completed bool) error {
	if cursor == nil {
		return nil
	}
	cursor.mu.Lock()
	defer cursor.mu.Unlock()
	return cursor.finishLocked(completed)
}

func (cursor *Cursor) finishLocked(completed bool) error {
	if cursor.finalized {
		return nil
	}
	cursor.finalized = true
	cursor.done = true
	var err error
	if cursor.rows != nil {
		err = cursor.rows.Close()
		cursor.rows = nil
	}
	if cursor.onFinish != nil {
		if finishErr := cursor.onFinish(completed && err == nil); err == nil {
			err = finishErr
		}
		cursor.onFinish = nil
	}
	return err
}

// NextBatch reads at most batchSize rows. done is true after the final batch.
func (cursor *Cursor) NextBatch(batchSize int) (rows [][]any, done bool, err error) {
	if cursor == nil {
		return nil, true, nil
	}
	cursor.mu.Lock()
	defer cursor.mu.Unlock()
	if cursor.done || cursor.rows == nil {
		return nil, true, nil
	}
	if cursor.ctx != nil {
		if err := cursor.ctx.Err(); err != nil {
			_ = cursor.finishLocked(false)
			return nil, true, err
		}
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize > MaxBatchSize {
		batchSize = MaxBatchSize
	}
	rows = make([][]any, 0, batchSize)
	var batchBytes int64
	for len(rows) < batchSize {
		if !cursor.rows.Next() {
			err = cursor.rows.Err()
			finishErr := cursor.finishLocked(err == nil)
			if err == nil {
				err = finishErr
			}
			return rows, true, err
		}
		values := make([]any, len(cursor.columns))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := cursor.rows.Scan(dest...); err != nil {
			_ = cursor.finishLocked(false)
			return rows, true, err
		}
		for i := range values {
			values[i] = detach(values[i])
		}
		rows = append(rows, values)
		batchBytes += estimateRowBytes(values)
		cursor.rowsRead++
		if batchBytes >= MaxBatchBytes {
			return rows, false, nil
		}
	}
	return rows, false, nil
}

func (cursor *Cursor) RowsRead() int64 {
	if cursor == nil {
		return 0
	}
	cursor.mu.Lock()
	defer cursor.mu.Unlock()
	return cursor.rowsRead
}

// Close finalizes a cursor and waits for an in-flight batch read. Cancel the
// context passed to Open first when NextBatch may be blocked in the driver.
func (cursor *Cursor) Close() error {
	return cursor.finish(false)
}

// Query starts a query and streams its rows in bounded batches. The caller
// owns cancellation and must read events until the channel closes or the
// context is canceled. A slow consumer applies backpressure instead of
// allowing an unbounded result queue to grow in memory.
func Query(ctx context.Context, db *sql.DB, statement string, batchSize int) <-chan Event {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize > MaxBatchSize {
		batchSize = MaxBatchSize
	}

	out := make(chan Event, 1)
	go stream(ctx, db, statement, batchSize, out)
	return out
}

func stream(ctx context.Context, db *sql.DB, statement string, batchSize int, out chan<- Event) {
	defer close(out)

	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		send(ctx, out, Event{Done: true, Err: err})
		return
	}
	defer rows.Close()

	names, err := rows.Columns()
	if err != nil {
		send(ctx, out, Event{Done: true, Err: err})
		return
	}
	columns := make([]Column, len(names))
	for i, name := range names {
		columns[i] = Column{Name: name}
	}
	if types, typeErr := rows.ColumnTypes(); typeErr == nil {
		for i := range columns {
			if i < len(types) {
				columns[i].DatabaseType = types[i].DatabaseTypeName()
			}
		}
	}
	if !send(ctx, out, Event{Columns: columns}) {
		return
	}

	var (
		batch      = make([][]any, 0, batchSize)
		batchBytes int64
		rowCount   int64
	)
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		ready := batch
		batch = make([][]any, 0, batchSize)
		batchBytes = 0
		return send(ctx, out, Event{Rows: ready, RowsRead: rowCount})
	}

	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			send(ctx, out, Event{Done: true, RowsRead: rowCount, Err: err})
			return
		}
		for i := range values {
			values[i] = detach(values[i])
		}
		batch = append(batch, values)
		batchBytes += estimateRowBytes(values)
		rowCount++
		if (len(batch) == batchSize || batchBytes >= MaxBatchBytes) && !flush() {
			return
		}
	}
	if err := rows.Err(); err != nil {
		if !flush() {
			return
		}
		send(ctx, out, Event{Done: true, RowsRead: rowCount, Err: err})
		return
	}
	if !flush() {
		return
	}
	send(ctx, out, Event{Done: true, RowsRead: rowCount})
}

func send(ctx context.Context, out chan<- Event, event Event) bool {
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func detach(value any) any {
	switch value := value.(type) {
	case []byte:
		return append([]byte(nil), value...)
	case time.Time:
		return value
	default:
		return value
	}
}
