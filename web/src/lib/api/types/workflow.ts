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
