export interface TreeNode {
  title: string
  key: string
  isLeaf: boolean
  children?: TreeNode[]
  /** 修改时间（Unix 秒） */
  mtime?: number
  /** 创建时间（Unix 秒），系统拿不到时后端不带，排序时按 mtime 兜底 */
  btime?: number
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
