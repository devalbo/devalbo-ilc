// Ambient types for the extras this pin adds on top of preview2-shim@0.19.
// Stock `_getFileData(): string` remains; we add the live-tree accessor.
declare module "@bytecodealliance/preview2-shim/filesystem" {
  export function _getFileDataTree(): {
    dir?: Record<string, unknown>;
    source?: Uint8Array | string;
  };
}
