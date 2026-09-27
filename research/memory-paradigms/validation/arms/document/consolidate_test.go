package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// passModel adds one line per pass citing the pass's first mid, read from
// the episodes it is shown; a pass listed in refuse first drops every line.
type passModel struct {
	refuse map[int64]bool
	calls  int
	shown  []string
}

func (p *passModel) Chat(_ context.Context, messages []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	p.calls++
	content := messages[1].Content
	p.shown = append(p.shown, content)
	var episodes []struct {
		Mid int64 `json:"mid"`
	}
	start := strings.Index(content, "New episodes:\n") + len("New episodes:\n")
	end := strings.Index(content[start:], "]") + start + 1
	if err := json.Unmarshal([]byte(content[start:end]), &episodes); err != nil {
		return agent.Response{}, err
	}
	ops := fmt.Sprintf(`{"ops":[{"op":"add","text":"fact from mid %d","mids":[%d]}]}`, episodes[0].Mid, episodes[0].Mid)
	lines := strings.Count(content[:start], "(mids ")
	if lines > 0 && !strings.Contains(content, "Attempt") && p.refuse[episodes[0].Mid] {
		var drops []string
		for i := 1; i <= lines; i++ {
			drops = append(drops, fmt.Sprintf(`{"op":"drop","line":%d}`, i))
		}
		ops = `{"ops":[` + strings.Join(drops, ",") + `]}`
	}
	return agent.Response{ToolCalls: []agent.ToolCall{{ID: "u", Name: updateTool, Input: json.RawMessage(ops)}}}, nil
}

func mids(n int) []Mid {
	var out []Mid
	for i := range n {
		out = append(out, Mid{ID: int64(i + 1), First: int64(4*i + 1), Last: int64(4*i + 4), Summary: fmt.Sprintf("summary %d", i+1)})
	}
	return out
}

func TestConsolidate_ReadsEightMidsAPassAndRecordsEachPass(t *testing.T) {
	model := &passModel{}
	var w bytes.Buffer
	doc, records, err := Consolidate(context.Background(), mids(20), nil, func(int) (agent.Model, error) { return model, nil }, &w)
	require.NoError(t, err)

	assert.Equal(t, 3, model.calls, "20 mids take passes of 8, 8 and 4")
	require.Len(t, records, 3)
	assert.Equal(t, []int64{17, 18, 19, 20}, records[2].Mids)
	assert.Equal(t, 3, strings.Count(w.String(), "\n"))
	assert.Equal(t, []string{"fact from mid 1", "fact from mid 9", "fact from mid 17"}, []string{doc.Lines[0].Text, doc.Lines[1].Text, doc.Lines[2].Text})
	assert.Contains(t, model.shown[2], "2. fact from mid 9 (mids 9)", "a pass is shown the document so far")
}

type failingModel struct{}

func (failingModel) Chat(context.Context, []agent.Message, []tool.Descriptor) (agent.Response, error) {
	return agent.Response{}, errors.New("no call expected")
}

func TestConsolidate_ReplaysARecordingWithoutACall(t *testing.T) {
	var w bytes.Buffer
	first, records, err := Consolidate(context.Background(), mids(20), nil, func(int) (agent.Model, error) { return &passModel{}, nil }, &w)
	require.NoError(t, err)

	again, _, err := Consolidate(context.Background(), mids(20), records, func(int) (agent.Model, error) { return failingModel{}, nil }, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, first, again)
	_, _, err = Consolidate(context.Background(), mids(20), records[:2], func(int) (agent.Model, error) { return failingModel{}, nil }, &bytes.Buffer{})
	assert.Error(t, err, "the control: a pass missing from the recording is sent")
}

func TestConsolidate_AsksAgainWithTheReasonWhenAPassIsRefused(t *testing.T) {
	model := &passModel{refuse: map[int64]bool{9: true}}
	doc, records, err := Consolidate(context.Background(), mids(16), nil, func(int) (agent.Model, error) { return model, nil }, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, 3, model.calls)
	assert.Equal(t, 2, records[1].Attempts)
	require.Len(t, records[1].Refusals, 1)
	assert.Contains(t, model.shown[2], "were refused", "the retry says why")
	assert.Len(t, doc.Lines, 2)
}

func TestParseOps_ReadsSeveralDocumentUpdateCallsAsOne(t *testing.T) {
	resp := agent.Response{ToolCalls: []agent.ToolCall{
		{Name: updateTool, Input: json.RawMessage(`{"ops":[{"op":"add","text":"a","mids":[1]}]}`)},
		{Name: updateTool, Input: json.RawMessage(`{"ops":[{"op":"drop","line":2}]}`)},
	}}
	ops, err := parseOps(resp)
	require.NoError(t, err)
	assert.Equal(t, []Op{{Op: "add", Text: "a", Mids: []int64{1}}, {Op: "drop", Line: 2}}, ops)
	_, err = parseOps(agent.Response{ToolCalls: []agent.ToolCall{{Name: "other", Input: json.RawMessage(`{}`)}}})
	assert.Error(t, err, "the control: an answer without document_update is refused")
}

func TestFirstOps_KeepsTheFirstSixteenInOrder(t *testing.T) {
	var ops []Op
	for i := range 20 {
		ops = append(ops, Op{Op: "drop", Line: i + 1})
	}
	kept, cut := firstOps(ops)
	assert.Len(t, kept, maxOps)
	assert.Equal(t, 4, cut)
	assert.Equal(t, 16, kept[15].Line)
	kept, cut = firstOps(ops[:3])
	assert.Len(t, kept, 3, "the control: a short answer is kept whole")
	assert.Zero(t, cut)
}
