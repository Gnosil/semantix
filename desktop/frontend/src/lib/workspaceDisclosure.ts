import type { RightDockMode } from "../store/layout";

type SessionDisclosure = { open: boolean; mode: RightDockMode; workSeq: number; handled: boolean };

/** Launch-local choices: opening yesterday's app does not restore an empty dock. */
export class WorkspaceDisclosure {
  private sessions = new Map<string, SessionDisclosure>();
  private active = "";

  visit(scope: string, workSeq: number): { changed: boolean; open: boolean; mode: RightDockMode } {
    const switched = this.active !== scope;
    this.active = scope;
    let session = this.sessions.get(scope);
    if (!session) {
      session = { open: false, mode: "context", workSeq, handled: false };
      this.sessions.set(scope, session);
    }
    let revealed = false;
    if (workSeq > session.workSeq && !session.handled) {
      session.open = true;
      session.mode = "context";
      session.handled = true;
      revealed = true;
    }
    session.workSeq = workSeq;
    return { changed: switched || revealed, open: session.open, mode: session.mode };
  }

  choose(open: boolean, mode: RightDockMode): void {
    const session = this.sessions.get(this.active);
    if (session) Object.assign(session, { open, mode, handled: true });
  }

  setMode(mode: RightDockMode): void {
    const session = this.sessions.get(this.active);
    if (session) session.mode = mode;
  }

  reset(scope: string, workSeq: number): void {
    this.sessions.set(scope, { open: false, mode: "context", workSeq, handled: false });
    this.active = scope;
  }
}
