import type { Transition, Variants } from 'motion/react'

const easeOut = [0.22, 1, 0.36, 1] as const
const easeIn = [0.32, 0, 0.67, 0] as const

export const fadeTransition: Transition = {
    duration: 0.2,
    ease: 'linear',
}

export const slideTransition: Transition = {
    duration: 0.3,
    ease: easeOut,
}

export const slideExitTransition: Transition = {
    duration: 0.24,
    ease: easeIn,
}

export const modalTransition: Transition = {
    duration: 0.22,
    ease: easeOut,
}

export const bubbleTransition: Transition = {
    duration: 0.25,
    ease: easeOut,
}

export const overlayVariants: Variants = {
    hidden: { opacity: 0, transition: fadeTransition },
    visible: { opacity: 1, transition: fadeTransition },
}

export const centerModalVariants: Variants = {
    hidden: { opacity: 0, scale: 0.92, y: 24, transition: modalTransition },
    visible: { opacity: 1, scale: 1, y: 0, transition: modalTransition },
}

export const bottomSheetVariants: Variants = {
    hidden: { y: '100%', transition: slideExitTransition },
    visible: { y: 0, transition: slideTransition },
}

export const drawerVariants: Variants = {
    hidden: { x: '100%', transition: slideExitTransition },
    visible: { x: 0, transition: slideTransition },
}

export const bubbleVariants: Variants = {
    hidden: { opacity: 0, y: 12, transition: bubbleTransition },
    visible: { opacity: 1, y: 0, transition: bubbleTransition },
}
