interface PendingImagesProps {
    pendingImages: string[]
    getImageUrl: (imagePath: string) => string
    onRemove: (index: number) => void
}

export const PendingImages: React.FC<PendingImagesProps> = ({ pendingImages, getImageUrl, onRemove }) => {
    const { t } = useT()
    if (pendingImages.length === 0) return null

    return (
        <div className="pending-image-list">
            {pendingImages.map((imagePath, index) => (
                <div key={`${imagePath}-${index}`} className="pending-image-item">
                    <img src={getImageUrl(imagePath)} alt={t('chat.pendingImages')} />
                    <button type="button" className="pending-image-remove" onClick={() => onRemove(index)}>
                        ×
                    </button>
                </div>
            ))}
        </div>
    )
}
import { useT } from '../../../contexts/I18nContext'
