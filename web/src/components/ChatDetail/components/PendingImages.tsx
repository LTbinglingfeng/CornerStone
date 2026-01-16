interface PendingImagesProps {
  pendingImages: string[]
  getImageUrl: (imagePath: string) => string
  onRemove: (index: number) => void
}

export const PendingImages: React.FC<PendingImagesProps> = ({ pendingImages, getImageUrl, onRemove }) => {
  if (pendingImages.length === 0) return null

  return (
    <div className="pending-image-list">
      {pendingImages.map((imagePath, index) => (
        <div key={`${imagePath}-${index}`} className="pending-image-item">
          <img src={getImageUrl(imagePath)} alt="待发送图片" />
          <button type="button" className="pending-image-remove" onClick={() => onRemove(index)}>
            ×
          </button>
        </div>
      ))}
    </div>
  )
}

