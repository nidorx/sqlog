package sqlog

type Tick struct {
	Index int   `json:"index"`
	Start int64 `json:"epoch_start"`
	End   int64 `json:"epoch_end"`
	Count int64 `json:"count"`
	Debug int64 `json:"debug"`
	Info  int64 `json:"info"`
	Warn  int64 `json:"warn"`
	Error int64 `json:"error"`
}

type TicksInput struct {
	Expr        string   `json:"expr"`
	Level       []string `json:"level"` // ["debug","info","warn","error"]
	EpochEnd    int64    `json:"epoch"`
	IntervalSec int      `json:"interval"`
	MaxResult   int      `json:"limit"`
}

type EntriesInput struct {
	Expr       string   `json:"expr"`
	Level      []string `json:"level"` // ["debug","info","warn","error"]
	Direction  string   `json:"dir"`   // before|after
	EpochStart int64    `json:"epoch"`
	NanosStart int      `json:"nanos"`
	MaxResult  int      `json:"limit"`
}

type Output struct {
	Scheduled bool    `json:"scheduled,omitempty"` // Indicates that this is a partial result
	TaskIds   []int32 `json:"tasks,omitempty"`     // The id so that the result can be retrieved in the future
	Error     error   `json:"-"`                   // The last error occurred
	Ticks     []*Tick `json:"ticks,omitempty"`     // The ticks available in this response
	Entries   []any   `json:"entries,omitempty"`   // The log records available in this response
}

func (l *sqlog) Entries(input *EntriesInput) (*Output, error) {
	if s, ok := l.storage.(StorageWithApi); ok {
		return s.Entries(input)
	}
	return &Output{}, nil
}

func (l *sqlog) Ticks(input *TicksInput) (*Output, error) {
	if s, ok := l.storage.(StorageWithApi); ok {
		return s.Ticks(input)
	}
	return &Output{}, nil
}

func (l *sqlog) Result(taskId int32) (*Output, error) {
	if s, ok := l.storage.(StorageWithApi); ok {
		return s.Result(taskId)
	}
	return &Output{}, nil
}

func (l *sqlog) Cancel(taskId int32) error {
	if s, ok := l.storage.(StorageWithApi); ok {
		return s.Cancel(taskId)
	}
	return nil
}
