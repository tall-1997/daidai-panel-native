export function getSessionGeneration(): number
export function advanceSessionGeneration(): number
export function isCurrentSession(generation: number): boolean
export function isRequestSessionCurrent(generation: number | undefined): generation is number
export function onSessionGenerationChange(listener: () => void): () => boolean
export function refreshSessionSingleFlight(refresh: () => Promise<string>, generation?: number): Promise<string>
