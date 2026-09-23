export function nameInitial(name: string | undefined): string {
  return (name || 'U').charAt(0).toUpperCase()
}
