export interface TreeNode {
  title: string
  key: string
  isLeaf: boolean
  children?: TreeNode[]
}

export interface ScriptVersionRecord {
  id: number
  version: number | string
  message: string
  content_length: number
  created_at: string
}

export interface ScriptVersionDetail extends ScriptVersionRecord {
  script_path: string
  content: string
}
