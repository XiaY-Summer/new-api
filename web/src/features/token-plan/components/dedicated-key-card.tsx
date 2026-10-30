/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { KeyRound, Loader2, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

type DedicatedKeyCardProps = {
  apiKey: string
  resetting?: boolean
  onReset: () => void
}

export function DedicatedKeyCard({
  apiKey,
  resetting,
  onReset,
}: DedicatedKeyCardProps) {
  const { t } = useTranslation()
  const [confirmOpen, setConfirmOpen] = useState(false)

  return (
    <>
      <Card>
        <CardHeader>
          <div className='flex items-start justify-between gap-3'>
            <div className='min-w-0'>
              <CardTitle className='flex items-center gap-2 text-base'>
                <KeyRound className='text-primary h-4 w-4' />
                {t('Dedicated API Key')}
              </CardTitle>
              <CardDescription className='mt-1 text-xs'>
                {t(
                  'Requests sent with this key are metered against your TOKEN PLAN and never touch your wallet balance.'
                )}
              </CardDescription>
            </div>
            <Button
              variant='outline'
              size='sm'
              disabled={resetting}
              onClick={() => setConfirmOpen(true)}
            >
              {resetting ? (
                <Loader2 className='h-3.5 w-3.5 animate-spin' />
              ) : (
                <RotateCcw className='h-3.5 w-3.5' />
              )}
              {t('Reset Key')}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className='bg-muted/50 flex items-center gap-2 rounded-md border px-3 py-2'>
            <code className='min-w-0 flex-1 truncate font-mono text-xs'>
              {apiKey || '-'}
            </code>
            {apiKey ? <CopyButton value={apiKey} /> : null}
          </div>
          <p className='text-muted-foreground mt-2 text-[11px]'>
            {t(
              'Only one dedicated key can exist. Resetting invalidates the previous key immediately.'
            )}
          </p>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Reset Key')}
        desc={t(
          'The current key will stop working immediately. Any client still using it must be updated.'
        )}
        confirmText={t('Reset Key')}
        destructive
        isLoading={resetting}
        handleConfirm={() => {
          onReset()
          setConfirmOpen(false)
        }}
      />
    </>
  )
}
