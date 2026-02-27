package input

import "context"

// Controller provides platform-specific keyboard and mouse input simulation.
type Controller interface {
	ExecuteCommands(ctx context.Context, commands []Command) error
	TypeText(ctx context.Context, text string) error
	Close() error
}

// Command represents a single input command.
type Command struct {
	Action string `json:"action" required:"true" description:"The input action to perform" enum:"key,keydown,keyup,type,click,buttondown,buttonup,wheel,hwheel,mouseto,mousemove,keydelay,keyhold,typedelay,typehold,sleep"`
	Args   string `json:"args" required:"true" description:"Arguments for the action"`
}

// CommandParameters is the schema for the input control function call.
type CommandParameters struct {
	Commands []Command `json:"commands" required:"true" description:"List of input commands to execute sequentially"`
	Summary  string    `json:"summary" required:"true" description:"A brief summary of what this command sequence is intended to do"`
}

// ScreenshotParameters is the schema for the take screenshot function call.
type ScreenshotParameters struct {
	Reason string `json:"reason" required:"true" description:"Why the screenshot is being taken"`
}
