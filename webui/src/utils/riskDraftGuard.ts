export interface RiskDraftTicket { botId: string; generation: number; sequence: number; revision: number }

/** Coordinates local draft ownership; it does not replace server-side revisions. */
export class RiskDraftGuard {
  private botId = ''
  private generation = 0
  private sequence = 0
  private revision = 0
  private dirty = false
  private saving = false
  private loaded = false
  private activeSave: RiskDraftTicket | undefined

  reset(botId: string): void {
    this.botId = botId
    this.generation++
    this.sequence++
    this.revision = 0
    this.dirty = this.saving = this.loaded = false
    this.activeSave = undefined
  }

  owns(ticket: RiskDraftTicket): boolean {
    return ticket.botId === this.botId && ticket.generation === this.generation
  }

  beginFetch(botId: string): RiskDraftTicket | undefined {
    if (botId !== this.botId) return undefined
    return { botId, generation: this.generation, sequence: ++this.sequence, revision: this.revision }
  }

  accepts(ticket: RiskDraftTicket): boolean {
    return this.owns(ticket) && ticket.sequence === this.sequence
  }

  acceptsDraft(ticket: RiskDraftTicket): boolean {
    if (!this.accepts(ticket) || this.dirty || this.saving) return false
    this.loaded = true
    return true
  }

  edit(botId: string): boolean {
    if (botId !== this.botId || !this.loaded) return false
    this.dirty = true
    this.revision++
    return true
  }

  beginSave(botId: string): RiskDraftTicket | undefined {
    if (botId !== this.botId || !this.loaded || this.saving) return undefined
    this.saving = true
    this.activeSave = { botId, generation: this.generation, sequence: ++this.sequence, revision: this.revision }
    return this.activeSave
  }

  finishSave(ticket: RiskDraftTicket, success: boolean): boolean {
    if (!this.owns(ticket) || this.activeSave !== ticket) return false
    this.activeSave = undefined
    this.saving = false
    this.sequence++ // In-flight polls may describe the configuration before this save.
    if (success && this.revision === ticket.revision) this.dirty = false
    return true
  }
}
