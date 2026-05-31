// WorkflowMeta describes a workflow file available for manual dispatch
// (GitHub-Actions style workflow_dispatch UI).
export interface WorkflowMeta {
  // short selector name (base file name without extension)
  name: string;
  // full config file name as stored in the forge
  file: string;
}
