// localStorage can be missing or throw (private windows, blocked site data): the app
// must work without it.

export function readStored(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function writeStored(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Not persisting a preference is acceptable.
  }
}
