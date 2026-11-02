export interface User {
  id: number
  username: string
  role: 'admin' | 'user'
  is_root: boolean
  disabled: boolean
  must_change_password: boolean
  created_at: number
  last_login_at: number | null
}

export type UploadState =
  | 'created'
  | 'received'
  | 'verifying'
  | 'complete'
  | 'failed_hash'
  | 'aborted'
  | 'expired'

export interface ServerUpload {
  id: string
  user_id: number
  owner_name: string
  original_name: string
  size_bytes: number
  bytes_received: number
  offset: number
  client_sha256: string
  server_sha256: string
  state: UploadState
  error: string
  created_at: number
  updated_at: number
  completed_at: number | null
  verified_bytes?: number
  stored_path?: string
}

export interface AppConfig {
  max_upload_bytes: number
  chunk_size_bytes: number
}
