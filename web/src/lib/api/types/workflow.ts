export type WorkflowInputType = 'string' | 'number' | 'boolean' | 'choice';

// WorkflowInput mirrors a single workflow_dispatch `inputs:` declaration.
export interface WorkflowInput {
  name: string;
  description?: string;
  type?: WorkflowInputType;
  required?: boolean;
  default?: unknown;
  options?: string[];
}

// WorkflowMeta describes a workflow file available for manual dispatch
// (GitHub-Actions style workflow_dispatch UI).
export interface WorkflowMeta {
  // short selector name (base file name without extension)
  name: string;
  // full config file name as stored in the forge
  file: string;
  // declared workflow_dispatch inputs, in file order
  inputs?: WorkflowInput[];
}
