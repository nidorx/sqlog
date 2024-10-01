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

// TicksInput dados para buscar de registros
type TicksInput struct {
	Expr        string   `json:"expr"`
	Level       []string `json:"level"` // ["debug","info","warn","error"]
	EpochEnd    int64    `json:"epoch"`
	IntervalSec int      `json:"interval"`
	MaxResult   int      `json:"limit"`
}

// ListInput dados para buscar de registros
type EntriesInput struct {
	Expr       string   `json:"expr"`
	Level      []string `json:"level"` // ["debug","info","warn","error"]
	Direction  string   `json:"dir"`   // before|after
	EpochStart int64    `json:"epoch"`
	NanosStart int      `json:"nanos"`
	MaxResult  int      `json:"limit"`
}

// TicksOutput resultado da consulta de Ticks
type Output struct {
	Scheduled bool    `json:"scheduled,omitempty"` // Indica que esse é um resultado parcial
	TaskIds   []int32 `json:"tasks,omitempty"`     // O id para consulta futura
	Error     error   `json:"-"`                   // O último erro ocorrido
	Ticks     []*Tick `json:"ticks,omitempty"`     // Os ticks disponíveis nessa resposta
	Entries   []any   `json:"entries,omitempty"`   // Os registros de log disponíveis nessa resposta
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
