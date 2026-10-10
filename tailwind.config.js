/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    './index.html',
    './404.html',
    './Library/index.html',
    './Workshop/index.html'
  ],
  darkMode: 'class',
  theme: {
    extend: {
      fontFamily: {
        serif: ['"Noto Serif SC"', 'SimSun', 'Songti SC', 'serif'],
        sans: ['"Noto Serif SC"', 'system-ui', 'sans-serif']
      },
      colors: {
        primary: '#d4af37',
        secondary: '#2d6a4f',
        accent: '#8a1c1c',
        dark: {
          950: '#020617',
          900: '#0a0f1e',
          850: '#141b2d',
          800: '#1e293b',
          700: '#334155'
        },
        mystic: {
          gold: '#d4af37',
          gold_dim: '#8a7120',
          green: '#2d6a4f',
          teal: '#115e59',
          red: '#8a1c1c',
          purple: '#581c87'
        }
      },
      boxShadow: {
        'glow-gold': '0 0 15px rgba(212, 175, 55, 0.15)',
        'glow-green': '0 0 15px rgba(45, 106, 79, 0.2)',
        'inner-glow': 'inset 0 0 20px rgba(0,0,0,0.5)'
      },
      backgroundImage: {
        'void-noise': "url('data:image/svg+xml,%3Csvg viewBox=%220 0 200 200%22 xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cfilter id=%22noiseFilter%22%3E%3CfeTurbulence type=%22fractalNoise%22 baseFrequency=%220.65%22 numOctaves=%223%22 stitchTiles=%22stitch%22/%3E%3C/filter%3E%3Crect width=%22100%25%22 height=%22100%25 filter=%22url(%23noiseFilter)%22 opacity=%220.07%22/%3E%3C/svg%3E')",
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))'
      },
      animation: {
        'fade-in': 'fadeIn 0.6s ease-out forwards',
        'fade-in-bg': 'fadeInBg 2s ease-out forwards',
        'slide-up': 'slideUp 0.6s cubic-bezier(0.16, 1, 0.3, 1)',
        'pulse-slow': 'pulse 4s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'message-in': 'messageIn 0.4s cubic-bezier(0.2, 0.8, 0.2, 1) forwards',
        'float': 'float 6s ease-in-out infinite'
      },
      keyframes: {
        fadeIn: { '0%': { opacity: '0' }, '100%': { opacity: '1' } },
        fadeInBg: { '0%': { opacity: '0' }, '100%': { opacity: '0.05' } },
        slideUp: { '0%': { transform: 'translateY(20px)', opacity: '0' }, '100%': { transform: 'translateY(0)', opacity: '1' } },
        messageIn: { '0%': { transform: 'translateY(15px)', opacity: '0' }, '100%': { transform: 'translateY(0)', opacity: '1' } },
        float: { '0%, 100%': { transform: 'translateY(0)' }, '50%': { transform: 'translateY(-5px)' } }
      }
    }
  }
};
