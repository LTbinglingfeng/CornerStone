interface MessageImagesProps {
    timestamp: string
    imagePaths: string[] | undefined
    getImageUrl: (imagePath: string) => string
}

export const MessageImages: React.FC<MessageImagesProps> = ({ timestamp, imagePaths, getImageUrl }) => {
    if (!imagePaths || imagePaths.length === 0) return null

    return (
        <div className="message-images">
            {imagePaths.map((imagePath, index) => (
                <img
                    key={`${timestamp}-image-${index}`}
                    src={getImageUrl(imagePath)}
                    alt="聊天图片"
                    className="message-image"
                />
            ))}
        </div>
    )
}
