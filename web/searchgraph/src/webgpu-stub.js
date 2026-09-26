// Build-time replacement for `three/webgpu`.
//
// three-render-objects imports WebGPURenderer statically but only uses it
// when constructed with `useWebGPU: true`, which this viewer never does.
// Aliasing the module to this stub keeps ~570 KB of dead renderer code out
// of the bundle. If a dependency upgrade ever starts using WebGPU by
// default, the constructor below throws, the viewer catches it and falls
// back to the 2D renderer, so the failure is visible rather than silent.
export class WebGPURenderer {
  constructor() {
    throw new Error("WebGPU renderer is not bundled in this viewer");
  }
}
