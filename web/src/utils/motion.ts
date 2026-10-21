import type { Transition, Variants } from 'motion/react'

// Strong ease-out for anything entering; exits are shorter so the UI never feels in the way.
const easeOut = [0.23, 1, 0.32, 1] as const
const easeDrawer = [0.32, 0.72, 0, 1] as const

export const fadeTransition: Transition = {
    duration: 0.16,
    ease: 'easeOut',
}

export const slideTransition: Transition = {
    duration: 0.32,
    ease: easeDrawer,
}

export const slideExitTransition: Transition = {
    duration: 0.2,
    ease: easeOut,
}

export const modalTransition: Transition = {
    duration: 0.2,
    ease: easeOut,
}

const modalExitTransition: Transition = {
    duration: 0.14,
    ease: easeOut,
}

export const bubbleTransition: Transition = {
    duration: 0.22,
    ease: easeOut,
}

export const overlayVariants: Variants = {
    hidden: { opacity: 0, transition: fadeTransition },
    visible: { opacity: 1, transition: fadeTransition },
}

// Modals stay centred (no trigger origin) and never grow from nothing.
export const centerModalVariants: Variants = {
    hidden: { opacity: 0, scale: 0.97, y: 6, transition: modalExitTransition },
    visible: { opacity: 1, scale: 1, y: 0, transition: modalTransition },
}

export const bottomSheetVariants: Variants = {
    hidden: { y: '100%', transition: slideExitTransition },
    visible: { y: 0, transition: slideTransition },
}

// Management pages are navigation destinations, not draggable sheets.
// Keep frequent navigation (including keyboard navigation) immediate.
export const drawerVariants: Variants = {
    hidden: { opacity: 0, transition: { duration: 0 } },
    visible: { opacity: 1, transition: { duration: 0 } },
}

export const bubbleVariants: Variants = {
    hidden: { opacity: 0, y: 8, transition: bubbleTransition },
    visible: { opacity: 1, y: 0, transition: bubbleTransition },
}
