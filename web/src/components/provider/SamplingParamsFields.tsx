import type { Provider } from '../../types/chat'
import { NumericInput } from '../NumericInput'
import { Toggle } from '../ui'
import { useT } from '../../contexts/I18nContext'

type SamplingField = 'temperature' | 'top_p' | 'temperature_enabled' | 'top_p_enabled'

interface SamplingParamsFieldsProps {
    provider: Provider
    onChange: (field: SamplingField, value: number | boolean) => void
}

/**
 * 温度 / Top P 均为可选参数：关闭时不向上游发送，适配不再支持采样参数的新模型。
 */
export default function SamplingParamsFields({ provider, onChange }: SamplingParamsFieldsProps) {
    const { t } = useT()
    const temperatureEnabled = Boolean(provider.temperature_enabled)
    const topPEnabled = Boolean(provider.top_p_enabled)
    const showAnthropicHint = provider.type === 'anthropic' && temperatureEnabled && topPEnabled

    return (
        <div className="provider-form-sampling">
            <div className="provider-form-switch">
                <span className="provider-form-switch__text">
                    <span className="modal-label">{t('provider.sendTemperature')}</span>
                    <span className="provider-form-switch__hint">{t('provider.samplingParamHint')}</span>
                </span>
                <Toggle
                    label={t('provider.sendTemperature')}
                    checked={temperatureEnabled}
                    onChange={(checked) => onChange('temperature_enabled', checked)}
                />
            </div>
            {temperatureEnabled && (
                <div className="modal-group">
                    <label className="modal-label">{t('provider.temperature')}</label>
                    <NumericInput
                        aria-label={t('provider.temperature')}
                        className="modal-input"
                        min={0}
                        max={2}
                        step={0.1}
                        value={provider.temperature}
                        parseAs="float"
                        onValueChange={(value) => onChange('temperature', value)}
                        placeholder="0.8"
                    />
                </div>
            )}

            <div className="provider-form-switch">
                <span className="provider-form-switch__text">
                    <span className="modal-label">{t('provider.sendTopP')}</span>
                    <span className="provider-form-switch__hint">{t('provider.samplingParamHint')}</span>
                </span>
                <Toggle
                    label={t('provider.sendTopP')}
                    checked={topPEnabled}
                    onChange={(checked) => onChange('top_p_enabled', checked)}
                />
            </div>
            {topPEnabled && (
                <div className="modal-group">
                    <label className="modal-label">{t('provider.topP')}</label>
                    <NumericInput
                        aria-label={t('provider.topP')}
                        className="modal-input"
                        min={0}
                        max={1}
                        step={0.1}
                        value={provider.top_p}
                        parseAs="float"
                        onValueChange={(value) => onChange('top_p', value)}
                        placeholder="1"
                    />
                </div>
            )}

            {showAnthropicHint && <p className="provider-form-switch__hint">{t('provider.anthropicSamplingHint')}</p>}
        </div>
    )
}
