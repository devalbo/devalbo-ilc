/** @module Interface devalbo:ilc/types **/
export interface CommandResult {
  success: boolean,
  output: Uint8Array,
  error?: string,
}
