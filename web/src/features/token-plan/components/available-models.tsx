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
import { Boxes } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

type AvailableModelsProps = {
  models: string[]
  unrestricted?: boolean
}

export function AvailableModels({
  models,
  unrestricted,
}: AvailableModelsProps) {
  const { t } = useTranslation()

  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center gap-2 text-base'>
          <Boxes className='text-primary h-4 w-4' />
          {t('Available Models')}
        </CardTitle>
        <CardDescription className='mt-1 text-xs'>
          {unrestricted
            ? t('This plan has no model restriction.')
            : t(
                'Only the models listed below can be called with the dedicated key.'
              )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {models.length ? (
          <div className='flex max-h-56 flex-wrap gap-1.5 overflow-y-auto'>
            {models.map((model) => (
              <Badge
                key={model}
                variant='outline'
                className='font-mono text-xs'
              >
                {model}
              </Badge>
            ))}
          </div>
        ) : (
          <p className='text-muted-foreground text-xs'>
            {t('No model is currently available for this plan.')}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
