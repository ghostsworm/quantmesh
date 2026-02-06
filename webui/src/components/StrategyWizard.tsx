import React, { useState, useCallback } from 'react';
import {
  Box,
  Stepper,
  Step,
  StepLabel,
  StepContent,
  Button,
  Typography,
  Card,
  CardContent,
  Grid,
  Slider,
  TextField,
  FormControl,
  FormControlLabel,
  InputLabel,
  Select,
  MenuItem,
  Switch,
  Chip,
  Alert,
  AlertTitle,
  CircularProgress,
  Divider,
  Paper,
  Tooltip,
  IconButton,
  Collapse,
} from '@mui/material';
import {
  TrendingUp,
  TrendingDown,
  ShowChart,
  Timeline,
  Security,
  Speed,
  AccountBalance,
  Warning,
  CheckCircle,
  Info,
  ExpandMore,
  ExpandLess,
  Refresh,
  Save,
  PlayArrow,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

// 策略類型定义
interface StrategyConfig {
  type: string;
  name: string;
  direction: 'LONG' | 'SHORT' | 'BOTH';
  parameters: Record<string, any>;
}

// 风險评估結果
interface RiskAssessment {
  overallScore: number;
  riskLevel: 'low' | 'medium' | 'high' | 'extreme';
  scoreBreakdown: {
    capitalManagement: number;
    riskControl: number;
    strategyFit: number;
    marketCondition: number;
  };
  warnings: string[];
  suggestions: Array<{
    title: string;
    description: string;
    priority: 'high' | 'medium' | 'low';
  }>;
  recommended: boolean;
}

// 預設模板
const STRATEGY_TEMPLATES = {
  conservative: {
    icon: <Security color="success" />,
    color: 'success.main',
    defaults: {
      maxLayers: 10,
      stopLoss: 5,
      takeProfit: 2,
      leverage: 3,
      priceStep: 2,
      multiplier: 1.2,
      trendFilter: true,
      cascadeProtection: true,
    },
  },
  balanced: {
    icon: <ShowChart color="warning" />,
    color: 'warning.main',
    defaults: {
      maxLayers: 20,
      stopLoss: 10,
      takeProfit: 3,
      leverage: 5,
      priceStep: 1.5,
      multiplier: 1.5,
      trendFilter: true,
      cascadeProtection: true,
    },
  },
  aggressive: {
    icon: <Speed color="error" />,
    color: 'error.main',
    defaults: {
      maxLayers: 30,
      stopLoss: 15,
      takeProfit: 5,
      leverage: 10,
      priceStep: 1,
      multiplier: 2,
      trendFilter: false,
      cascadeProtection: true,
    },
  },
};

// 策略類型
const STRATEGY_TYPES = [
  {
    type: 'dca',
    icon: <Timeline />,
    featureKeys: ['dynamicSpacing', 'tripleTP', 'cascadeProtection'],
  },
  {
    type: 'martingale',
    icon: <TrendingDown />,
    featureKeys: ['doubleDown', 'riskDecrement', 'reverseMartingale'],
  },
  {
    type: 'combo',
    icon: <AccountBalance />,
    featureKeys: ['longShortHedge', 'adaptiveWeight', 'allMarketCoverage'],
  },
  {
    type: 'trend',
    icon: <TrendingUp />,
    featureKeys: ['trendIdentification', 'dynamicTP', 'trailingStop'],
  },
];

interface StrategyWizardProps {
  onComplete: (config: StrategyConfig) => void;
  onCancel: () => void;
  initialConfig?: StrategyConfig;
}

const StrategyWizard: React.FC<StrategyWizardProps> = ({
  onComplete,
  onCancel,
  initialConfig,
}) => {
  const { t } = useTranslation();
  const [activeStep, setActiveStep] = useState(0);
  const [template, setTemplate] = useState<string>('balanced');
  const [strategyType, setStrategyType] = useState<string>('dca');
  const [direction, setDirection] = useState<'LONG' | 'SHORT' | 'BOTH'>('LONG');
  const [symbol, setSymbol] = useState<string>('BTCUSDT');
  const [capital, setCapital] = useState<number>(1000);
  const [parameters, setParameters] = useState<Record<string, any>>(
    STRATEGY_TEMPLATES.balanced.defaults
  );
  const [riskAssessment, setRiskAssessment] = useState<RiskAssessment | null>(null);
  const [isAssessing, setIsAssessing] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // 步骤定义
  const steps = [
    { label: t('strategyWizard.steps.style.label'), description: t('strategyWizard.steps.style.description') },
    { label: t('strategyWizard.steps.strategy.label'), description: t('strategyWizard.steps.strategy.description') },
    { label: t('strategyWizard.steps.params.label'), description: t('strategyWizard.steps.params.description') },
    { label: t('strategyWizard.steps.riskAssessment.label'), description: t('strategyWizard.steps.riskAssessment.description') },
    { label: t('strategyWizard.steps.confirm.label'), description: t('strategyWizard.steps.confirm.description') },
  ];

  // 处理模板选擇
  const handleTemplateSelect = useCallback((templateKey: string) => {
    setTemplate(templateKey);
    const templateConfig = STRATEGY_TEMPLATES[templateKey as keyof typeof STRATEGY_TEMPLATES];
    setParameters(templateConfig.defaults);
  }, []);

  // 处理参數变化
  const handleParamChange = useCallback((key: string, value: any) => {
    setParameters((prev) => ({ ...prev, [key]: value }));
  }, []);

  // 執行风險评估
  const runRiskAssessment = useCallback(async () => {
    setIsAssessing(true);
    try {
      // 模拟 API 調用
      await new Promise((resolve) => setTimeout(resolve, 1500));
      
      // 计算风險评分
      let score = 100;
      const warnings: string[] = [];
      const suggestions: RiskAssessment['suggestions'] = [];

      // 评估杠杆
      if (parameters.leverage > 10) {
        score -= 20;
        warnings.push(t('strategyWizard.warnings.highLeverage'));
      } else if (parameters.leverage > 5) {
        score -= 10;
      }

      if (!parameters.stopLoss || parameters.stopLoss <= 0) {
        score -= 25;
        warnings.push(t('strategyWizard.warnings.noStopLoss'));
        suggestions.push({
          title: t('strategyWizard.suggestions.addStopLoss'),
          description: t('strategyWizard.suggestions.addStopLossDesc'),
          priority: 'high',
        });
      } else if (parameters.stopLoss > 20) {
        score -= 10;
        suggestions.push({
          title: t('strategyWizard.suggestions.adjustStopLoss'),
          description: t('strategyWizard.suggestions.adjustStopLossDesc'),
          priority: 'medium',
        });
      }

      if (parameters.maxLayers > 30) {
        score -= 15;
        warnings.push(t('strategyWizard.warnings.tooManyLayers'));
      }

      if (!parameters.trendFilter) {
        score -= 5;
        suggestions.push({
          title: t('strategyWizard.suggestions.enableTrendFilter'),
          description: t('strategyWizard.suggestions.enableTrendFilterDesc'),
          priority: 'medium',
        });
      }

      // 确保评分在有效範圍内
      score = Math.max(0, Math.min(100, score));

      const riskLevel: RiskAssessment['riskLevel'] = 
        score >= 80 ? 'low' :
        score >= 60 ? 'medium' :
        score >= 40 ? 'high' : 'extreme';

      setRiskAssessment({
        overallScore: score,
        riskLevel,
        scoreBreakdown: {
          capitalManagement: Math.min(25, Math.round(score * 0.25)),
          riskControl: Math.min(25, Math.round(score * 0.25)),
          strategyFit: Math.min(25, Math.round(score * 0.25)),
          marketCondition: Math.min(25, Math.round(score * 0.25)),
        },
        warnings,
        suggestions,
        recommended: score >= 60,
      });
    } catch (error) {
      console.error('Risk assessment failed:', error);
    } finally {
      setIsAssessing(false);
    }
  }, [parameters]);

  // 下一步
  const handleNext = useCallback(() => {
    if (activeStep === 3 && !riskAssessment) {
      runRiskAssessment();
    }
    setActiveStep((prev) => prev + 1);
  }, [activeStep, riskAssessment, runRiskAssessment]);

  // 上一步
  const handleBack = useCallback(() => {
    setActiveStep((prev) => prev - 1);
  }, []);

  // 完成配置
  const handleComplete = useCallback(() => {
    const config: StrategyConfig = {
      type: strategyType,
      name: `${t(`strategyWizard.strategyTypes.${strategyType}.name`)}_${symbol}`,
      direction,
      parameters: {
        symbol,
        capital,
        ...parameters,
      },
    };
    onComplete(config);
  }, [strategyType, symbol, direction, capital, parameters, onComplete]);

  // 獲取风險等级颜色
  const getRiskColor = (level: string) => {
    switch (level) {
      case 'low': return 'success';
      case 'medium': return 'warning';
      case 'high': return 'error';
      case 'extreme': return 'error';
      default: return 'default';
    }
  };

  // 渲染步骤内容
  const renderStepContent = (step: number) => {
    switch (step) {
      case 0:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              {t('strategyWizard.selectTradingStyle')}
            </Typography>
            <Grid container spacing={2}>
              {Object.entries(STRATEGY_TEMPLATES).map(([key, value]) => (
                <Grid item xs={12} md={4} key={key}>
                  <Card
                    sx={{
                      cursor: 'pointer',
                      border: template === key ? 2 : 1,
                      borderColor: template === key ? value.color : 'divider',
                      transition: 'all 0.3s',
                      '&:hover': {
                        transform: 'translateY(-4px)',
                        boxShadow: 3,
                      },
                    }}
                    onClick={() => handleTemplateSelect(key)}
                  >
                    <CardContent>
                      <Box display="flex" alignItems="center" mb={1}>
                        {value.icon}
                        <Typography variant="h6" ml={1}>
                          {t(`strategyWizard.templates.${key}.name`)}
                        </Typography>
                      </Box>
                      <Typography variant="body2" color="text.secondary">
                        {t(`strategyWizard.templates.${key}.description`)}
                      </Typography>
                      {template === key && (
                        <Chip
                          label={t('strategyWizard.selected')}
                          size="small"
                          color="primary"
                          sx={{ mt: 1 }}
                        />
                      )}
                    </CardContent>
                  </Card>
                </Grid>
              ))}
            </Grid>
          </Box>
        );

      case 1:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              {t('strategyWizard.selectStrategyType')}
            </Typography>
            <Grid container spacing={2}>
              {STRATEGY_TYPES.map((strategy) => (
                <Grid item xs={12} md={6} key={strategy.type}>
                  <Card
                    sx={{
                      cursor: 'pointer',
                      border: strategyType === strategy.type ? 2 : 1,
                      borderColor: strategyType === strategy.type ? 'primary.main' : 'divider',
                      transition: 'all 0.3s',
                      '&:hover': {
                        transform: 'translateY(-4px)',
                        boxShadow: 3,
                      },
                    }}
                    onClick={() => setStrategyType(strategy.type)}
                  >
                    <CardContent>
                      <Box display="flex" alignItems="center" mb={1}>
                        {strategy.icon}
                        <Typography variant="h6" ml={1}>
                          {t(`strategyWizard.strategyTypes.${strategy.type}.name`)}
                        </Typography>
                      </Box>
                      <Typography variant="body2" color="text.secondary" mb={2}>
                        {t(`strategyWizard.strategyTypes.${strategy.type}.description`)}
                      </Typography>
                      <Box display="flex" gap={1} flexWrap="wrap">
                        {strategy.featureKeys.map((featureKey) => (
                          <Chip
                            key={featureKey}
                            label={t(`strategyWizard.features.${featureKey}`)}
                            size="small"
                            variant="outlined"
                          />
                        ))}
                      </Box>
                    </CardContent>
                  </Card>
                </Grid>
              ))}
            </Grid>

            <Box mt={3}>
              <Grid container spacing={2}>
                <Grid item xs={12} md={6}>
                  <FormControl fullWidth>
                    <InputLabel>{t('strategyWizard.tradingPair')}</InputLabel>
                    <Select
                      value={symbol}
                      label={t('strategyWizard.tradingPair')}
                      onChange={(e) => setSymbol(e.target.value)}
                    >
                      <MenuItem value="BTCUSDT">BTC/USDT</MenuItem>
                      <MenuItem value="ETHUSDT">ETH/USDT</MenuItem>
                      <MenuItem value="BNBUSDT">BNB/USDT</MenuItem>
                      <MenuItem value="SOLUSDT">SOL/USDT</MenuItem>
                    </Select>
                  </FormControl>
                </Grid>
                <Grid item xs={12} md={6}>
                  <FormControl fullWidth>
                    <InputLabel>{t('strategyWizard.tradingDirection')}</InputLabel>
                    <Select
                      value={direction}
                      label={t('strategyWizard.tradingDirection')}
                      onChange={(e) => setDirection(e.target.value as any)}
                    >
                      <MenuItem value="LONG">{t('strategyWizard.longOnly')}</MenuItem>
                      <MenuItem value="SHORT">{t('strategyWizard.shortOnly')}</MenuItem>
                      <MenuItem value="BOTH">{t('strategyWizard.longShortBoth')}</MenuItem>
                    </Select>
                  </FormControl>
                </Grid>
              </Grid>
            </Box>
          </Box>
        );

      case 2:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              {t('strategyWizard.configureParams')}
            </Typography>
            
            <Paper sx={{ p: 3, mb: 3 }}>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('strategyWizard.capitalConfig')}
              </Typography>
              <Grid container spacing={3}>
                <Grid item xs={12} md={6}>
                  <TextField
                    fullWidth
                    label={t('strategyWizard.capitalInput')}
                    type="number"
                    value={capital}
                    onChange={(e) => setCapital(Number(e.target.value))}
                    InputProps={{ inputProps: { min: 100 } }}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>{t('strategyWizard.leverageMultiplier')}: {parameters.leverage}x</Typography>
                  <Slider
                    value={parameters.leverage}
                    onChange={(_, v) => handleParamChange('leverage', v)}
                    min={1}
                    max={20}
                    marks={[
                      { value: 1, label: '1x' },
                      { value: 5, label: '5x' },
                      { value: 10, label: '10x' },
                      { value: 20, label: '20x' },
                    ]}
                  />
                </Grid>
              </Grid>
            </Paper>

            <Paper sx={{ p: 3, mb: 3 }}>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('strategyWizard.riskControl')}
              </Typography>
              <Grid container spacing={3}>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>{t('strategyWizard.stopLossRatio')}: {parameters.stopLoss}%</Typography>
                  <Slider
                    value={parameters.stopLoss}
                    onChange={(_, v) => handleParamChange('stopLoss', v)}
                    min={1}
                    max={30}
                    marks={[
                      { value: 5, label: '5%' },
                      { value: 15, label: '15%' },
                      { value: 30, label: '30%' },
                    ]}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>{t('strategyWizard.takeProfitRatio')}: {parameters.takeProfit}%</Typography>
                  <Slider
                    value={parameters.takeProfit}
                    onChange={(_, v) => handleParamChange('takeProfit', v)}
                    min={0.5}
                    max={10}
                    step={0.5}
                    marks={[
                      { value: 1, label: '1%' },
                      { value: 5, label: '5%' },
                      { value: 10, label: '10%' },
                    ]}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>{t('strategyWizard.maxLayers')}: {parameters.maxLayers}</Typography>
                  <Slider
                    value={parameters.maxLayers}
                    onChange={(_, v) => handleParamChange('maxLayers', v)}
                    min={5}
                    max={50}
                    marks={[
                      { value: 10, label: '10' },
                      { value: 30, label: '30' },
                      { value: 50, label: '50' },
                    ]}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>{t('strategyWizard.priceSpacing')}: {parameters.priceStep}%</Typography>
                  <Slider
                    value={parameters.priceStep}
                    onChange={(_, v) => handleParamChange('priceStep', v)}
                    min={0.5}
                    max={5}
                    step={0.1}
                    marks={[
                      { value: 1, label: '1%' },
                      { value: 2, label: '2%' },
                      { value: 5, label: '5%' },
                    ]}
                  />
                </Grid>
              </Grid>
            </Paper>

            <Paper sx={{ p: 3 }}>
              <Box display="flex" justifyContent="space-between" alignItems="center">
                <Typography variant="subtitle1" fontWeight="bold">
                  {t('strategyWizard.advancedSettings')}
                </Typography>
                <IconButton onClick={() => setShowAdvanced(!showAdvanced)}>
                  {showAdvanced ? <ExpandLess /> : <ExpandMore />}
                </IconButton>
              </Box>
              <Collapse in={showAdvanced}>
                <Grid container spacing={2} mt={1}>
                  <Grid item xs={12} md={6}>
                    <FormControlLabel
                      control={
                        <Switch
                          checked={parameters.trendFilter}
                          onChange={(e) => handleParamChange('trendFilter', e.target.checked)}
                        />
                      }
                      label={t('strategyWizard.trendFilter')}
                    />
                  </Grid>
                  <Grid item xs={12} md={6}>
                    <FormControlLabel
                      control={
                        <Switch
                          checked={parameters.cascadeProtection}
                          onChange={(e) => handleParamChange('cascadeProtection', e.target.checked)}
                        />
                      }
                      label={t('strategyWizard.cascadeProtectionSwitch')}
                    />
                  </Grid>
                  {strategyType === 'martingale' && (
                    <Grid item xs={12} md={6}>
                      <Typography gutterBottom>
                        {t('strategyWizard.positionMultiplier')}: {parameters.multiplier}x
                      </Typography>
                      <Slider
                        value={parameters.multiplier}
                        onChange={(_, v) => handleParamChange('multiplier', v)}
                        min={1}
                        max={3}
                        step={0.1}
                        marks={[
                          { value: 1, label: '1x' },
                          { value: 2, label: '2x' },
                          { value: 3, label: '3x' },
                        ]}
                      />
                    </Grid>
                  )}
                </Grid>
              </Collapse>
            </Paper>
          </Box>
        );

      case 3:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              {t('strategyWizard.aiRiskAssessment')}
            </Typography>
            
            {isAssessing ? (
              <Box display="flex" flexDirection="column" alignItems="center" py={4}>
                <CircularProgress size={60} />
                <Typography mt={2}>{t('strategyWizard.analyzingStrategy')}</Typography>
              </Box>
            ) : riskAssessment ? (
              <Box>
                <Paper sx={{ p: 3, mb: 3, textAlign: 'center' }}>
                  <Typography variant="h2" fontWeight="bold">
                    {riskAssessment.overallScore}
                  </Typography>
                  <Chip
                    label={t(`strategyWizard.riskLevels.${riskAssessment.riskLevel}`)}
                    color={getRiskColor(riskAssessment.riskLevel) as any}
                    sx={{ mt: 1 }}
                  />
                </Paper>

                <Grid container spacing={2} mb={3}>
                  {Object.entries(riskAssessment.scoreBreakdown).map(([key, value]) => (
                    <Grid item xs={6} md={3} key={key}>
                      <Paper sx={{ p: 2, textAlign: 'center' }}>
                        <Typography variant="h4">{value}/25</Typography>
                        <Typography variant="body2" color="text.secondary">
                          {t(`strategyWizard.scoreBreakdown.${key}`)}
                        </Typography>
                      </Paper>
                    </Grid>
                  ))}
                </Grid>

                {riskAssessment.warnings.length > 0 && (
                  <Alert severity="warning" sx={{ mb: 2 }}>
                    <AlertTitle>{t('strategyWizard.warnings.title')}</AlertTitle>
                    <ul style={{ margin: 0, paddingLeft: 20 }}>
                      {riskAssessment.warnings.map((warning, i) => (
                        <li key={i}>{warning}</li>
                      ))}
                    </ul>
                  </Alert>
                )}

                {riskAssessment.suggestions.length > 0 && (
                  <Alert severity="info" sx={{ mb: 2 }}>
                    <AlertTitle>{t('strategyWizard.suggestions.title')}</AlertTitle>
                    <ul style={{ margin: 0, paddingLeft: 20 }}>
                      {riskAssessment.suggestions.map((suggestion, i) => (
                        <li key={i}>
                          <strong>{suggestion.title}</strong>: {suggestion.description}
                        </li>
                      ))}
                    </ul>
                  </Alert>
                )}

                <Box display="flex" justifyContent="center" mt={2}>
                  <Button
                    variant="outlined"
                    startIcon={<Refresh />}
                    onClick={runRiskAssessment}
                  >
                    {t('strategyWizard.reassess')}
                  </Button>
                </Box>
              </Box>
            ) : (
              <Box textAlign="center" py={4}>
                <Button
                  variant="contained"
                  size="large"
                  onClick={runRiskAssessment}
                  >
                    {t('strategyWizard.startRiskAssessment')}
                  </Button>
              </Box>
            )}
          </Box>
        );

      case 4:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              {t('strategyWizard.confirmStrategyConfig')}
            </Typography>
            
            <Paper sx={{ p: 3, mb: 3 }}>
              <Grid container spacing={2}>
                <Grid item xs={6}>
                  <Typography color="text.secondary">{t('strategyWizard.strategyType')}</Typography>
                  <Typography variant="h6">
                    {t(`strategyWizard.strategyTypes.${strategyType}.name`)}
                  </Typography>
                </Grid>
                <Grid item xs={6}>
                  <Typography color="text.secondary">{t('strategyWizard.tradingPair')}</Typography>
                  <Typography variant="h6">{symbol}</Typography>
                </Grid>
                <Grid item xs={6}>
                  <Typography color="text.secondary">{t('strategyWizard.tradingDirection')}</Typography>
                  <Typography variant="h6">
                    {direction === 'LONG' ? t('strategyWizard.directionLong') : direction === 'SHORT' ? t('strategyWizard.directionShort') : t('strategyWizard.directionBoth')}
                  </Typography>
                </Grid>
                <Grid item xs={6}>
                  <Typography color="text.secondary">{t('strategyWizard.capitalInvested')}</Typography>
                  <Typography variant="h6">{capital} USDT</Typography>
                </Grid>
              </Grid>
              
              <Divider sx={{ my: 2 }} />
              
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                {t('strategyWizard.coreParams')}
              </Typography>
              <Grid container spacing={2}>
                <Grid item xs={4}>
                  <Typography color="text.secondary">{t('strategyWizard.leverage')}</Typography>
                  <Typography>{parameters.leverage}x</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">{t('strategyWizard.stopLoss')}</Typography>
                  <Typography>{parameters.stopLoss}%</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">{t('strategyWizard.takeProfit')}</Typography>
                  <Typography>{parameters.takeProfit}%</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">{t('strategyWizard.maxLayers')}</Typography>
                  <Typography>{parameters.maxLayers}</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">{t('strategyWizard.priceStep')}</Typography>
                  <Typography>{parameters.priceStep}%</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">{t('strategyWizard.trendFilterLabel')}</Typography>
                  <Typography>{parameters.trendFilter ? t('strategyWizard.enabled') : t('strategyWizard.disabled')}</Typography>
                </Grid>
              </Grid>
            </Paper>

            {riskAssessment && (
              <Alert
                severity={riskAssessment.recommended ? 'success' : 'warning'}
                icon={riskAssessment.recommended ? <CheckCircle /> : <Warning />}
              >
                {t('strategyWizard.riskScore')}: {riskAssessment.overallScore}/100 - 
                {riskAssessment.recommended ? ` ${t('strategyWizard.recommendStart')}` : ` ${t('strategyWizard.recommendOptimize')}`}
              </Alert>
            )}
          </Box>
        );

      default:
        return null;
    }
  };

  return (
    <Box sx={{ maxWidth: 900, mx: 'auto', p: 3 }}>
      <Typography variant="h4" gutterBottom fontWeight="bold">
        {t('strategyWizard.title')}
      </Typography>
      <Typography color="text.secondary" mb={3}>
        {t('strategyWizard.subtitle')}
      </Typography>

      <Stepper activeStep={activeStep} orientation="vertical">
        {steps.map((step, index) => (
          <Step key={step.label}>
            <StepLabel>
              <Typography variant="subtitle1">{step.label}</Typography>
              <Typography variant="body2" color="text.secondary">
                {step.description}
              </Typography>
            </StepLabel>
            <StepContent>
              <Box sx={{ mb: 2 }}>
                {renderStepContent(index)}
              </Box>
              <Box sx={{ mb: 2, display: 'flex', gap: 1 }}>
                <Button
                  disabled={index === 0}
                  onClick={handleBack}
                >
                  {t('strategyWizard.previousStep')}
                </Button>
                {index === steps.length - 1 ? (
                  <Button
                    variant="contained"
                    onClick={handleComplete}
                    startIcon={<PlayArrow />}
                    color="success"
                  >
                    {t('strategyWizard.startStrategy')}
                  </Button>
                ) : (
                  <Button
                    variant="contained"
                    onClick={handleNext}
                  >
                    {t('strategyWizard.nextStep')}
                  </Button>
                )}
                <Button onClick={onCancel} color="inherit">
                  {t('strategyWizard.cancel')}
                </Button>
              </Box>
            </StepContent>
          </Step>
        ))}
      </Stepper>
    </Box>
  );
};

export default StrategyWizard;
