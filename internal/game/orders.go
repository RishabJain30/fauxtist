package game

// CommandType is one of the six real command kinds a player can resolve, plus
// Hold (the no-op that fills unused slots and stands in for timeouts).
type CommandType string

const (
	CmdMarch         CommandType = "march"
	CmdFortify       CommandType = "fortify"
	CmdRecruit       CommandType = "recruit"
	CmdBuildFortress CommandType = "build_fortress"
	CmdBuildMine     CommandType = "build_mine"
	CmdHold          CommandType = "hold"
)

// Command is one order. Fields not relevant to a given Type are zero:
//   - March:          From, To (adjacent), Armies (1..3)
//   - Fortify:        To (owned non-capital)
//   - Recruit:        To (owned capital or fortress)
//   - Build Fortress: To (owned normal tile or unused mine site)
//   - Build Mine:     To (owned unused mine site)
//   - Hold:           no fields
type Command struct {
	Type   CommandType `json:"type"`
	From   TileID      `json:"from,omitempty"`
	To     TileID      `json:"to,omitempty"`
	Armies int         `json:"armies,omitempty"`
}

// EnergyCost returns the energy a command reserves. March and Hold are free.
func (c Command) EnergyCost() int {
	switch c.Type {
	case CmdFortify:
		return CostFortify
	case CmdRecruit:
		return CostRecruit
	case CmdBuildFortress:
		return CostBuildFortress
	case CmdBuildMine:
		return CostBuildMine
	default: // March, Hold, and any unknown type reserve nothing
		return 0
	}
}

// HoldCommand is the canonical no-op used to fill unused real command slots
// and to stand in for a player who let a deadline pass.
func HoldCommand() Command { return Command{Type: CmdHold} }

// Declaration is the single command a player commits during DECLARATION and
// that is shown publicly at DECLARATION_REVEAL. Submitted distinguishes a
// real player choice from an auto-Hold produced by a timeout: only a
// submitted, non-Hold declaration may later be turned into a Faux Order.
type Declaration struct {
	Command   Command `json:"command"`
	Submitted bool    `json:"submitted"`
}

// OrderSet is a player's private SECRET_PLANNING draft: the hidden real
// commands plus whether their public declaration is a Faux Order. Commands
// holds only the HIDDEN commands — two if the declaration is real (the
// declaration itself is the player's first real command), three if it is
// Faux (the declaration executes nothing, so all three real commands are
// hidden). Draft submission always replaces this whole set atomically; the
// engine never mutates one command at a time.
type OrderSet struct {
	Faux      bool      `json:"faux"`
	Commands  []Command `json:"commands"`
	Locked    bool      `json:"locked"`
	Submitted bool      `json:"submitted"`
}

// clone returns a deep copy of the order set.
func (o OrderSet) clone() OrderSet {
	o.Commands = append([]Command(nil), o.Commands...)
	return o
}

// HiddenCommandCount is how many hidden real commands a player must submit
// during SECRET_PLANNING given whether they marked their declaration Faux: a
// Faux declaration executes nothing, so all RealCommandSlots are hidden;
// otherwise the declaration is the first real command and the remaining
// slots are hidden.
func HiddenCommandCount(faux bool) int {
	if faux {
		return RealCommandSlots
	}
	return RealCommandSlots - 1
}

// realCommands assembles a player's full ordered list of real commands for
// resolution, padded to exactly RealCommandSlots with Holds. A Faux
// declaration contributes nothing; a real (submitted, non-Hold) declaration
// is the first command, followed by the hidden orders. Missing pieces (no
// declaration, no orders, short lists) become Holds so every player always
// resolves exactly RealCommandSlots commands.
func (s *State) realCommands(id PlayerID) []Command {
	out := make([]Command, 0, RealCommandSlots)
	order, hasOrders := s.Orders[id]
	if !hasOrders || !order.Faux {
		if decl, ok := s.Declarations[id]; ok && decl.Submitted && decl.Command.Type != CmdHold {
			out = append(out, decl.Command)
		} else {
			out = append(out, HoldCommand())
		}
	}
	if hasOrders {
		out = append(out, order.Commands...)
	}
	for len(out) < RealCommandSlots {
		out = append(out, HoldCommand())
	}
	return out[:RealCommandSlots]
}
