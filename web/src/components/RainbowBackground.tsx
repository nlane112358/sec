import React, { useEffect, useRef } from 'react'

export type RainbowMode = 'aurora' | 'vibrant' | 'subtle' | 'off'

interface RainbowBackgroundProps {
  mode: RainbowMode
  darkMode: boolean
}

export const RainbowBackground: React.FC<RainbowBackgroundProps> = ({ mode, darkMode }) => {
  const cursorRef = useRef<HTMLDivElement>(null)
  const targetPos = useRef({ x: -1000, y: -1000 })
  const currentPos = useRef({ x: -1000, y: -1000 })
  const animationFrameId = useRef<number | null>(null)
  const isMoving = useRef(false)
  const idleTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Smooth lerp mouse follower for ambient interactive spotlight
  useEffect(() => {
    if (mode === 'off') return

    const handleMouseMove = (e: MouseEvent) => {
      targetPos.current = { x: e.clientX, y: e.clientY }
      if (!isMoving.current) {
        isMoving.current = true
        animateCursor()
      }
      if (idleTimeout.current) clearTimeout(idleTimeout.current)
      idleTimeout.current = setTimeout(() => {
        isMoving.current = false
      }, 3000)
    }

    const animateCursor = () => {
      const dx = targetPos.current.x - currentPos.current.x
      const dy = targetPos.current.y - currentPos.current.y

      // Smooth lag / spring interpolation
      currentPos.current.x += dx * 0.08
      currentPos.current.y += dy * 0.08

      if (cursorRef.current) {
        cursorRef.current.style.transform = `translate3d(${currentPos.current.x - 220}px, ${
          currentPos.current.y - 220
        }px, 0)`
      }

      if (isMoving.current || Math.abs(dx) > 0.5 || Math.abs(dy) > 0.5) {
        animationFrameId.current = requestAnimationFrame(animateCursor)
      } else {
        isMoving.current = false
      }
    }

    window.addEventListener('mousemove', handleMouseMove, { passive: true })

    return () => {
      window.removeEventListener('mousemove', handleMouseMove)
      if (animationFrameId.current) cancelAnimationFrame(animationFrameId.current)
      if (idleTimeout.current) clearTimeout(idleTimeout.current)
    }
  }, [mode])

  if (mode === 'off') {
    return null
  }

  // Adjust opacity & saturation based on mode and dark/light theme
  const getContainerClasses = () => {
    switch (mode) {
      case 'vibrant':
        return darkMode
          ? 'opacity-95 saturate-[1.35]'
          : 'opacity-70 saturate-[1.25]'
      case 'subtle':
        return darkMode
          ? 'opacity-40 saturate-[0.85]'
          : 'opacity-30 saturate-[0.8]'
      case 'aurora':
      default:
        return darkMode
          ? 'opacity-85 saturate-[1.1]'
          : 'opacity-50 saturate-[1.0]'
    }
  }

  // Hue shift duration based on mode
  const getHueDurationClass = () => {
    switch (mode) {
      case 'vibrant':
        return 'animate-rainbow-fast'
      case 'subtle':
        return 'animate-rainbow-slow'
      case 'aurora':
      default:
        return 'animate-rainbow-normal'
    }
  }

  return (
    <div
      aria-hidden="true"
      className="fixed inset-0 pointer-events-none -z-10 overflow-hidden select-none"
    >
      {/* Deep cosmic base layer matching Google Gemini dark theme */}
      <div
        className={`absolute inset-0 transition-colors duration-500 ${
          darkMode
            ? 'bg-[#030611]'
            : 'bg-[#f8fafc]'
        }`}
      />

      {/* Main Changing Rainbow Aurora Mesh Layer */}
      <div
        className={`absolute inset-0 transition-opacity duration-700 ${getContainerClasses()} ${getHueDurationClass()}`}
      >
        {/* Orb 1: Sapphire Blue to Cyan (Top-Center / Left Drift) */}
        <div
          className="rainbow-orb absolute w-[72vw] h-[58vh] -top-[12vh] left-[14vw] rounded-full filter blur-[90px] sm:blur-[120px] will-change-transform animate-orb-float-1"
          style={{
            background: darkMode
              ? 'radial-gradient(circle at 45% 45%, rgba(37, 99, 235, 0.85) 0%, rgba(6, 182, 212, 0.55) 42%, rgba(2, 132, 199, 0) 75%)'
              : 'radial-gradient(circle at 45% 45%, rgba(96, 165, 250, 0.55) 0%, rgba(34, 211, 238, 0.35) 45%, rgba(56, 189, 248, 0) 75%)',
          }}
        />

        {/* Orb 2: Royal Indigo & Violet (Mid-Right Sweep) */}
        <div
          className="rainbow-orb absolute w-[65vw] h-[65vh] top-[18vh] -right-[10vw] rounded-full filter blur-[95px] sm:blur-[130px] will-change-transform animate-orb-float-2"
          style={{
            background: darkMode
              ? 'radial-gradient(circle at 50% 50%, rgba(124, 58, 237, 0.8) 0%, rgba(99, 102, 241, 0.6) 38%, rgba(79, 70, 229, 0) 75%)'
              : 'radial-gradient(circle at 50% 50%, rgba(167, 139, 250, 0.5) 0%, rgba(129, 140, 248, 0.35) 40%, rgba(99, 102, 241, 0) 75%)',
          }}
        />

        {/* Orb 3: Fuchsia, Magenta & Sunset Rose (Mid-Left / Bottom Sweep) */}
        <div
          className="rainbow-orb absolute w-[60vw] h-[55vh] top-[40vh] -left-[12vw] rounded-full filter blur-[85px] sm:blur-[120px] will-change-transform animate-orb-float-3"
          style={{
            background: darkMode
              ? 'radial-gradient(circle at 45% 55%, rgba(217, 70, 239, 0.75) 0%, rgba(244, 63, 94, 0.5) 42%, rgba(225, 29, 72, 0) 75%)'
              : 'radial-gradient(circle at 45% 55%, rgba(244, 114, 182, 0.45) 0%, rgba(251, 113, 133, 0.35) 45%, rgba(244, 63, 94, 0) 75%)',
          }}
        />

        {/* Orb 4: Gemini Center Glow Aura (Luminous halo behind center search & hero) */}
        <div
          className="rainbow-orb absolute w-[80vw] h-[48vh] top-[15vh] left-[10vw] rounded-full filter blur-[80px] sm:blur-[110px] will-change-transform animate-orb-pulse"
          style={{
            background: darkMode
              ? 'radial-gradient(ellipse at 50% 50%, rgba(59, 130, 246, 0.7) 0%, rgba(139, 92, 246, 0.48) 35%, rgba(236, 72, 153, 0.28) 60%, rgba(30, 27, 75, 0) 80%)'
              : 'radial-gradient(ellipse at 50% 50%, rgba(96, 165, 250, 0.45) 0%, rgba(192, 132, 252, 0.3) 40%, rgba(244, 114, 182, 0.15) 65%, rgba(255, 255, 255, 0) 80%)',
          }}
        />

        {/* Orb 5: Warm Amber Gold & Emerald Cyan (Bottom Shimmer) */}
        <div
          className="rainbow-orb absolute w-[55vw] h-[52vh] top-[55vh] left-[25vw] rounded-full filter blur-[95px] sm:blur-[125px] will-change-transform animate-orb-float-4"
          style={{
            background: darkMode
              ? 'radial-gradient(circle at 50% 50%, rgba(245, 158, 11, 0.55) 0%, rgba(16, 185, 129, 0.4) 40%, rgba(6, 182, 212, 0) 75%)'
              : 'radial-gradient(circle at 50% 50%, rgba(251, 191, 36, 0.35) 0%, rgba(52, 211, 153, 0.25) 40%, rgba(45, 212, 191, 0) 75%)',
          }}
        />

        {/* Interactive Mouse Rainbow Spotlight Orb */}
        <div
          ref={cursorRef}
          className="rainbow-orb absolute w-[440px] h-[440px] rounded-full filter blur-[80px] sm:blur-[100px] will-change-transform pointer-events-none opacity-60 sm:opacity-75 transition-opacity duration-300"
          style={{
            transform: 'translate3d(-1000px, -1000px, 0)',
            background: darkMode
              ? 'radial-gradient(circle at 50% 50%, rgba(139, 92, 246, 0.45) 0%, rgba(59, 130, 246, 0.3) 38%, rgba(236, 72, 153, 0.18) 60%, rgba(0, 0, 0, 0) 80%)'
              : 'radial-gradient(circle at 50% 50%, rgba(167, 139, 250, 0.35) 0%, rgba(96, 165, 250, 0.22) 40%, rgba(244, 114, 182, 0.1) 65%, rgba(255, 255, 255, 0) 80%)',
          }}
        />
      </div>

      {/* Atmospheric Vignette overlay to anchor contrast and frame content */}
      <div
        className="absolute inset-0 pointer-events-none"
        style={{
          background: darkMode
            ? 'radial-gradient(ellipse at 50% 35%, transparent 20%, rgba(3, 6, 17, 0.4) 65%, rgba(3, 6, 17, 0.85) 100%)'
            : 'radial-gradient(ellipse at 50% 35%, transparent 35%, rgba(248, 250, 252, 0.45) 75%, rgba(248, 250, 252, 0.85) 100%)',
        }}
      />

      {/* Microscopic Velvet Noise Film Grain to eliminate color banding */}
      <div
        className="absolute inset-0 pointer-events-none opacity-[0.032] mix-blend-overlay dark:opacity-[0.045]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noiseFilter'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.85' numOctaves='3' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noiseFilter)'/%3E%3C/svg%3E")`,
        }}
      />
    </div>
  )
}
