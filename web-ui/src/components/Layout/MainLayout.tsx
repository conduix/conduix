import { useState } from 'react'
import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import {
  Box,
  Drawer,
  AppBar,
  Toolbar,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  IconButton,
  Avatar,
  Menu,
  MenuItem,
  Divider,
  Select,
  FormControl,
  SelectChangeEvent,
  useTheme,
} from '@mui/material'
import {
  Dashboard as DashboardIcon,
  AccountTree as BranchesIcon,
  Schedule as ScheduleIcon,
  Dns as ClusterIcon,
  Computer as DesktopIcon,
  History as HistoryIcon,
  Person as UserIcon,
  Logout as LogoutIcon,
  Menu as MenuIcon,
  ChevronLeft as ChevronLeftIcon,
  Group as TeamIcon,
  Folder as ProjectIcon,
  Language as LanguageIcon,
  DarkMode as DarkModeIcon,
  LightMode as LightModeIcon,
} from '@mui/icons-material'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '../../store/auth'
import { useThemeStore } from '../../store/theme'

const drawerWidth = 240
const collapsedDrawerWidth = 64

export default function MainLayout() {
  const { t, i18n } = useTranslation()
  const [collapsed, setCollapsed] = useState(false)
  const [anchorEl, setAnchorEl] = useState<null | HTMLElement>(null)
  const navigate = useNavigate()
  const location = useLocation()
  const { user, logout } = useAuthStore()
  const { mode, toggleMode } = useThemeStore()
  const theme = useTheme()

  const baseMenuItems = [
    {
      key: '/dashboard',
      icon: <DashboardIcon />,
      label: t('nav.dashboard'),
    },
    {
      key: '/projects',
      icon: <ProjectIcon />,
      label: t('nav.projects'),
    },
    {
      key: '/workflows',
      icon: <BranchesIcon />,
      label: t('nav.workflows'),
    },
    {
      key: '/schedules',
      icon: <ScheduleIcon />,
      label: t('nav.schedules'),
    },
    {
      key: '/clusters',
      icon: <ClusterIcon />,
      label: t('nav.clusters'),
    },
    {
      key: '/agents',
      icon: <DesktopIcon />,
      label: t('nav.agents'),
    },
    {
      key: '/history',
      icon: <HistoryIcon />,
      label: t('nav.history'),
    },
  ]

  const adminMenuItems = [
    {
      key: '/users',
      icon: <TeamIcon />,
      label: t('nav.users'),
    },
  ]

  const menuItems = user?.role === 'admin'
    ? [...baseMenuItems, ...adminMenuItems]
    : baseMenuItems

  const handleMenuClick = (key: string) => {
    navigate(key)
  }

  const handleLogout = () => {
    logout()
    navigate('/login')
    handleCloseUserMenu()
  }

  const handleProfile = () => {
    navigate('/profile')
    handleCloseUserMenu()
  }

  const handleLanguageChange = (event: SelectChangeEvent) => {
    i18n.changeLanguage(event.target.value)
  }

  const handleOpenUserMenu = (event: React.MouseEvent<HTMLElement>) => {
    setAnchorEl(event.currentTarget)
  }

  const handleCloseUserMenu = () => {
    setAnchorEl(null)
  }

  const currentWidth = collapsed ? collapsedDrawerWidth : drawerWidth

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      <Drawer
        variant="permanent"
        sx={{
          width: currentWidth,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: currentWidth,
            boxSizing: 'border-box',
            transition: theme.transitions.create('width', {
              easing: theme.transitions.easing.sharp,
              duration: theme.transitions.duration.enteringScreen,
            }),
            overflowX: 'hidden',
            bgcolor: theme.palette.mode === 'dark' ? '#001529' : '#001529',
          },
        }}
      >
        <Box
          sx={{
            height: 40,
            m: 2,
            background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
            borderRadius: 2,
            display: 'flex',
            alignItems: 'center',
            justifyContent: collapsed ? 'center' : 'flex-start',
            px: collapsed ? 0 : 1.5,
            gap: 1,
            color: '#fff',
            fontWeight: 'bold',
            fontSize: 16,
            boxShadow: '0 2px 8px rgba(102, 126, 234, 0.4)',
          }}
        >
          <img
            src="/favicon-24.ico"
            alt="Conduix"
            style={{ width: 20, height: 20 }}
          />
          {!collapsed && 'Conduix'}
        </Box>
        <List>
          {menuItems.map((item) => (
            <ListItem key={item.key} disablePadding sx={{ display: 'block' }}>
              <ListItemButton
                selected={location.pathname === item.key}
                onClick={() => handleMenuClick(item.key)}
                sx={{
                  minHeight: 48,
                  justifyContent: collapsed ? 'center' : 'initial',
                  px: 2.5,
                  '&.Mui-selected': {
                    bgcolor: 'primary.main',
                    '&:hover': {
                      bgcolor: 'primary.dark',
                    },
                  },
                  color: 'rgba(255, 255, 255, 0.65)',
                  '&:hover': {
                    bgcolor: 'rgba(255, 255, 255, 0.08)',
                  },
                }}
              >
                <ListItemIcon
                  sx={{
                    minWidth: 0,
                    mr: collapsed ? 0 : 3,
                    justifyContent: 'center',
                    color: location.pathname === item.key ? '#fff' : 'rgba(255, 255, 255, 0.65)',
                  }}
                >
                  {item.icon}
                </ListItemIcon>
                {!collapsed && (
                  <ListItemText
                    primary={item.label}
                    sx={{
                      '& .MuiListItemText-primary': {
                        color: location.pathname === item.key ? '#fff' : 'rgba(255, 255, 255, 0.65)',
                      },
                    }}
                  />
                )}
              </ListItemButton>
            </ListItem>
          ))}
        </List>
      </Drawer>

      <Box sx={{ flexGrow: 1, display: 'flex', flexDirection: 'column' }}>
        <AppBar
          position="static"
          color="default"
          elevation={0}
          sx={{
            bgcolor: theme.palette.background.paper,
            borderBottom: `1px solid ${theme.palette.divider}`,
          }}
        >
          <Toolbar>
            <IconButton
              edge="start"
              onClick={() => setCollapsed(!collapsed)}
              sx={{ mr: 2 }}
            >
              {collapsed ? <MenuIcon /> : <ChevronLeftIcon />}
            </IconButton>

            <Box sx={{ flexGrow: 1 }} />

            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
              <IconButton onClick={toggleMode} size="small">
                {mode === 'dark' ? <LightModeIcon /> : <DarkModeIcon />}
              </IconButton>

              <FormControl size="small" sx={{ minWidth: 100 }}>
                <Select
                  value={i18n.language}
                  onChange={handleLanguageChange}
                  IconComponent={LanguageIcon}
                  sx={{ '& .MuiSelect-icon': { right: 7 } }}
                >
                  <MenuItem value="en">English</MenuItem>
                  <MenuItem value="ko">한국어</MenuItem>
                </Select>
              </FormControl>

              <Box
                onClick={handleOpenUserMenu}
                sx={{
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1,
                }}
              >
                <Avatar src={user?.avatarUrl} sx={{ width: 32, height: 32 }}>
                  {!user?.avatarUrl && <UserIcon />}
                </Avatar>
                <span>{user?.name || user?.email}</span>
              </Box>

              <Menu
                anchorEl={anchorEl}
                open={Boolean(anchorEl)}
                onClose={handleCloseUserMenu}
                anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                transformOrigin={{ vertical: 'top', horizontal: 'right' }}
              >
                <MenuItem onClick={handleProfile}>
                  <ListItemIcon>
                    <UserIcon fontSize="small" />
                  </ListItemIcon>
                  Profile
                </MenuItem>
                <Divider />
                <MenuItem onClick={handleLogout}>
                  <ListItemIcon>
                    <LogoutIcon fontSize="small" />
                  </ListItemIcon>
                  {t('auth.logout')}
                </MenuItem>
              </Menu>
            </Box>
          </Toolbar>
        </AppBar>

        <Box
          component="main"
          sx={{
            flexGrow: 1,
            p: 3,
            bgcolor: theme.palette.mode === 'dark' ? theme.palette.background.default : '#f5f5f5',
          }}
        >
          <Box
            sx={{
              bgcolor: theme.palette.background.paper,
              borderRadius: 2,
              p: 3,
              minHeight: 'calc(100vh - 140px)',
            }}
          >
            <Outlet />
          </Box>
        </Box>
      </Box>
    </Box>
  )
}
