package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/garamsh/sherlock/internal/tool"
)

// The agent's memory files are two bounded documents it rewrites itself
// and sees at the head of every turn, as Hermes keeps MEMORY.md and
// USER.md. Its skills are procedures it writes for a kind of task: the
// name and one-line description of each are shown every turn, and the
// body only when the agent loads it, as Claude Code and Hermes load a
// skill. Both live in the memory store beside the conversation, so they
// survive restarts and the rolling window.
const (
	// MemoryFile holds what the agent keeps about its work.
	MemoryFile = "MEMORY.md"
	// UserFile holds who the person is and what they asked to be kept.
	UserFile = "USER.md"

	// MemoryFileWriteTool replaces one memory file whole.
	MemoryFileWriteTool = "memory_file_write"
	// SkillViewTool returns one skill's body.
	SkillViewTool = "skill_view"
	// SkillWriteTool creates or replaces one skill.
	SkillWriteTool = "skill_write"
)

// memoryFileLimits bound each memory file in characters: Hermes's
// defaults. A write past the bound is refused, so the agent consolidates
// rather than the runtime cutting.
var memoryFileLimits = map[string]int{MemoryFile: 2200, UserFile: 1375}

const (
	maxSkills                 = 16
	maxSkillNameLength        = 64
	maxSkillDescriptionLength = 200
	maxSkillBodyLength        = 4000
	skillPrefix               = "skills/"
)

var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// MemoryFileWriteDescriptor is the reserved memory-file write as the model
// sees it.
func MemoryFileWriteDescriptor() tool.Descriptor {
	return tool.Descriptor{
		Name: MemoryFileWriteTool,
		Description: "Replace one of your memory files with its whole new content. " +
			"USER.md (at most 1375 characters) holds who the person is and every standing instruction, preference and correction they gave. " +
			"MEMORY.md (at most 2200 characters) holds your current work with its goal, status and next step, and what you learned about your environment. " +
			"Both are shown to you at the start of every turn; the conversation is not. A write over the limit is refused: consolidate and write again.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"file":{"type":"string","enum":["MEMORY.md","USER.md"]},"content":{"type":"string"}},"required":["file","content"],"additionalProperties":false}`),
	}
}

// SkillViewDescriptor is the reserved skill load as the model sees it.
func SkillViewDescriptor() tool.Descriptor {
	return tool.Descriptor{
		Name:        SkillViewTool,
		Description: "Load the full instructions of one of the skills listed under \"Your skills\". Load a skill before a task its description covers.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1}},"required":["name"],"additionalProperties":false}`),
	}
}

// SkillWriteDescriptor is the reserved skill write as the model sees it.
func SkillWriteDescriptor() tool.Descriptor {
	return tool.Descriptor{
		Name: SkillWriteTool,
		Description: "Create or replace a skill: how a kind of task is to be done, such as the steps and conventions the person wants for it. " +
			"name is kebab-case; description is one line saying when the skill applies, shown to you every turn; body is the full procedure, loaded with skill_view. " +
			"At most 16 skills; the description holds 200 characters and the body 4000.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":64},"description":{"type":"string","minLength":1,"maxLength":200},"body":{"type":"string","minLength":1,"maxLength":4000}},"required":["name","description","body"],"additionalProperties":false}`),
	}
}

// memorySection renders the memory files and the skill index under the
// instructions, stating an empty file apart so the model knows it has
// written nothing rather than that the file is missing.
func memorySection(files map[string]string) string {
	out := "\n\n## Your memory files"
	for _, name := range []string{UserFile, MemoryFile} {
		content := files[name]
		if content == "" {
			content = "(empty)"
		}
		out += "\n### " + name + "\n" + content
	}
	out += "\n\n## Your skills"
	skills := skillNames(files)
	if len(skills) == 0 {
		return out + "\n(none)"
	}
	for _, name := range skills {
		description, _ := splitSkill(files[skillPrefix+name+".md"])
		out += "\n- " + name + ": " + description
	}
	return out
}

func skillNames(files map[string]string) []string {
	var names []string
	for path := range files {
		if name, ok := strings.CutPrefix(path, skillPrefix); ok {
			names = append(names, strings.TrimSuffix(name, ".md"))
		}
	}
	sort.Strings(names)
	return names
}

// A skill is stored as its description line, a blank line, then its body.
func joinSkill(description, body string) string { return description + "\n\n" + body }

func splitSkill(content string) (description, body string) {
	description, body, _ = strings.Cut(content, "\n\n")
	return description, body
}

func writeMemoryFile(ctx context.Context, store ConversationMemory, input json.RawMessage) (string, error) {
	var request struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return "", err
	}
	limit, ok := memoryFileLimits[request.File]
	if !ok {
		return "", fmt.Errorf("file must be %s or %s", MemoryFile, UserFile)
	}
	content := strings.TrimSpace(request.Content)
	if n := len([]rune(content)); n > limit {
		return "", fmt.Errorf("%s is %d characters, at most %d: consolidate it and write again", request.File, n, limit)
	}
	if err := store.SetAgentFile(ctx, request.File, content); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s saved, %d of %d characters", request.File, len([]rune(content)), limit), nil
}

func viewSkill(ctx context.Context, store ConversationMemory, input json.RawMessage) (string, error) {
	var request struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return "", err
	}
	files, err := store.AgentFiles(ctx)
	if err != nil {
		return "", err
	}
	content, ok := files[skillPrefix+strings.TrimSpace(request.Name)+".md"]
	if !ok {
		return "", fmt.Errorf("no skill named %q; the skills you have are listed under \"Your skills\"", request.Name)
	}
	_, body := splitSkill(content)
	return body, nil
}

func writeSkill(ctx context.Context, store ConversationMemory, input json.RawMessage) (string, error) {
	var request struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return "", err
	}
	name := strings.TrimSpace(request.Name)
	description := strings.Join(strings.Fields(request.Description), " ")
	body := strings.TrimSpace(request.Body)
	switch {
	case len(name) > maxSkillNameLength || !skillName.MatchString(name):
		return "", errors.New("name must be kebab-case, such as weekly-report, of at most 64 characters")
	case description == "" || len([]rune(description)) > maxSkillDescriptionLength:
		return "", fmt.Errorf("description must be one line of 1 to %d characters", maxSkillDescriptionLength)
	case body == "" || len([]rune(body)) > maxSkillBodyLength:
		return "", fmt.Errorf("body must hold 1 to %d characters", maxSkillBodyLength)
	}
	files, err := store.AgentFiles(ctx)
	if err != nil {
		return "", err
	}
	path := skillPrefix + name + ".md"
	if _, exists := files[path]; !exists && len(skillNames(files)) >= maxSkills {
		return "", fmt.Errorf("you have %d skills, the most you can keep: replace or merge one instead", maxSkills)
	}
	if err := store.SetAgentFile(ctx, path, joinSkill(description, body)); err != nil {
		return "", err
	}
	return "skill " + name + " saved", nil
}
