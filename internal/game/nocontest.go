package game

// EndNoContest ends an in-progress match with no winner — used when the sole
// remaining connected player chooses not to keep waiting. It sets a
// no-contest result and transitions straight to GAME_OVER.
func (e *Engine) EndNoContest() error {
	if e.state.Phase == PhaseLobby || e.state.Phase == PhaseGameOver {
		return ErrWrongPhase
	}
	e.state.finishGame(VictoryNoContest, nil)
	e.state.Phase = PhaseGameOver
	return nil
}
