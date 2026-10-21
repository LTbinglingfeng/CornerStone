import { useT } from '../contexts/I18nContext'
import { Icon } from './ui'
import './SearchBar.css'

interface SearchBarProps {
    value: string
    onChange: (value: string) => void
    placeholder?: string
}

const SearchBar: React.FC<SearchBarProps> = ({ value, onChange, placeholder }) => {
    const { t } = useT()
    const label = placeholder || t('chat.searchPlaceholder')
    return (
        <div className="search-bar" role="search">
            <Icon name="search" className="search-icon" size={15} />
            <input
                type="search"
                className="search-input"
                placeholder={label}
                aria-label={label}
                value={value}
                onChange={(e) => onChange(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Escape' && value) {
                        e.preventDefault()
                        onChange('')
                    }
                }}
            />
            {value && (
                <button
                    type="button"
                    className="search-clear"
                    onClick={() => onChange('')}
                    aria-label={t('common.clear')}
                >
                    <Icon name="x" size={14} />
                </button>
            )}
        </div>
    )
}

export default SearchBar
