export type PacketStep = 'idle' | 'opening' | 'opened'

export type RedPacketReceivedRecord = {
    receiverName: string
    senderName: string
    timestamp: string
}
