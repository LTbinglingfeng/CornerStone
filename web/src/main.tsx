import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { MotionConfig } from 'motion/react'
import { I18nProvider } from './contexts/I18nContext'
import { ThemeProvider } from './contexts/ThemeContext'
import { ToastProvider } from './contexts/ToastContext'
import { ConfirmProvider } from './contexts/ConfirmContext'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
        <BrowserRouter>
            <I18nProvider>
                <ThemeProvider>
                    <ToastProvider>
                        <ConfirmProvider>
                            <MotionConfig reducedMotion="user">
                                <App />
                            </MotionConfig>
                        </ConfirmProvider>
                    </ToastProvider>
                </ThemeProvider>
            </I18nProvider>
        </BrowserRouter>
    </React.StrictMode>
)
