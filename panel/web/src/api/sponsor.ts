import request from './request'

export interface SponsorRecord {
  id: number
  name: string
  amount: number
  avatar_url: string
  initial: string
  created_at: string
  updated_at: string
}

export interface SponsorSummary {
  sponsors: SponsorRecord[]
  count: number
  total_amount: number
  updated_at: string | null
  unavailable?: boolean
}

export const sponsorApi = {
  list() {
    return request.get('/sponsors') as Promise<{ data: SponsorSummary }>
  }
}
