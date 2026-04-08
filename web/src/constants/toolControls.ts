export type ToolControlSection = 'interaction' | 'realtime'
export const CORNERSTONE_WEB_SEARCH_TOOL_KEY = 'cornerstone_web_search'
const LEGACY_WEB_SEARCH_TOOL_KEY = 'web_search'
export type ToolControlSectionTitleKey = 'settings.toolSectionInteraction' | 'settings.toolSectionRealtime'
export type ToolControlTitleKey =
    | 'settings.toolSendRedPacket'
    | 'settings.toolRedPacketReceived'
    | 'settings.toolSendPat'
    | 'settings.toolNoReply'
    | 'settings.toolGetTime'
    | 'settings.toolGetWeather'
    | 'settings.toolScheduleReminder'
    | 'settings.toolWriteMemory'
    | 'settings.toolWebSearch'
export type ToolControlDescriptionKey =
    | 'settings.toolSendRedPacketDescription'
    | 'settings.toolRedPacketReceivedDescription'
    | 'settings.toolSendPatDescription'
    | 'settings.toolNoReplyDescription'
    | 'settings.toolGetTimeDescription'
    | 'settings.toolGetWeatherDescription'
    | 'settings.toolScheduleReminderDescription'
    | 'settings.toolWriteMemoryDescription'
    | 'settings.toolWebSearchDescription'
export type ToolControlHintKey = 'settings.toolWebSearchHint'

export interface ToolControlDefinition {
    key: string
    section: ToolControlSection
    titleKey: ToolControlTitleKey
    descriptionKey: ToolControlDescriptionKey
    hintKey?: ToolControlHintKey
}

export const TOOL_CONTROL_SECTION_TITLE_KEYS: Record<ToolControlSection, ToolControlSectionTitleKey> = {
    interaction: 'settings.toolSectionInteraction',
    realtime: 'settings.toolSectionRealtime',
}

export const TOOL_CONTROL_DEFINITIONS: ToolControlDefinition[] = [
    {
        key: 'send_red_packet',
        section: 'interaction',
        titleKey: 'settings.toolSendRedPacket',
        descriptionKey: 'settings.toolSendRedPacketDescription',
    },
    {
        key: 'red_packet_received',
        section: 'interaction',
        titleKey: 'settings.toolRedPacketReceived',
        descriptionKey: 'settings.toolRedPacketReceivedDescription',
    },
    {
        key: 'send_pat',
        section: 'interaction',
        titleKey: 'settings.toolSendPat',
        descriptionKey: 'settings.toolSendPatDescription',
    },
    {
        key: 'no_reply',
        section: 'interaction',
        titleKey: 'settings.toolNoReply',
        descriptionKey: 'settings.toolNoReplyDescription',
    },
    {
        key: 'get_time',
        section: 'realtime',
        titleKey: 'settings.toolGetTime',
        descriptionKey: 'settings.toolGetTimeDescription',
    },
    {
        key: 'get_weather',
        section: 'realtime',
        titleKey: 'settings.toolGetWeather',
        descriptionKey: 'settings.toolGetWeatherDescription',
    },
    {
        key: 'schedule_reminder',
        section: 'realtime',
        titleKey: 'settings.toolScheduleReminder',
        descriptionKey: 'settings.toolScheduleReminderDescription',
    },
    {
        key: 'write_memory',
        section: 'interaction',
        titleKey: 'settings.toolWriteMemory',
        descriptionKey: 'settings.toolWriteMemoryDescription',
    },
    {
        key: CORNERSTONE_WEB_SEARCH_TOOL_KEY,
        section: 'realtime',
        titleKey: 'settings.toolWebSearch',
        descriptionKey: 'settings.toolWebSearchDescription',
        hintKey: 'settings.toolWebSearchHint',
    },
]

export function createDefaultToolToggles(): Record<string, boolean> {
    return Object.fromEntries(TOOL_CONTROL_DEFINITIONS.map((tool) => [tool.key, true]))
}

export function normalizeToolToggles(toolToggles?: Record<string, boolean> | null): Record<string, boolean> {
    const normalized = createDefaultToolToggles()
    if (!toolToggles) {
        return normalized
    }

    for (const tool of TOOL_CONTROL_DEFINITIONS) {
        if (typeof toolToggles[tool.key] === 'boolean') {
            normalized[tool.key] = toolToggles[tool.key]
        }
    }
    if (
        typeof toolToggles[CORNERSTONE_WEB_SEARCH_TOOL_KEY] !== 'boolean' &&
        typeof toolToggles[LEGACY_WEB_SEARCH_TOOL_KEY] === 'boolean'
    ) {
        normalized[CORNERSTONE_WEB_SEARCH_TOOL_KEY] = toolToggles[LEGACY_WEB_SEARCH_TOOL_KEY]
    }

    return normalized
}

export function countEnabledToolToggles(toolToggles?: Record<string, boolean> | null): number {
    const normalized = normalizeToolToggles(toolToggles)
    return TOOL_CONTROL_DEFINITIONS.reduce((count, tool) => count + (normalized[tool.key] ? 1 : 0), 0)
}
