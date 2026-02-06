import React, { useState, useEffect } from 'react';
import {
  Box,
  VStack,
  HStack,
  Heading,
  Text,
  Badge,
  Button,
  useToast,
  CircularProgress,
  CircularProgressLabel,
  Icon,
  List,
  ListItem,
  Container,
  Card,
  CardBody,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
} from '@chakra-ui/react';
import { WarningIcon, CheckCircleIcon, InfoIcon } from '@chakra-ui/icons';
import {
  Radar,
  RadarChart,
  PolarGrid,
  PolarAngleAxis,
  ResponsiveContainer,
} from 'recharts';
import { getNewbieRiskCheck, applyNewbieSecurityConfig, NewbieRiskReport, NewbieRiskCheckItem } from '../services/api';
import { useTranslation } from 'react-i18next';

const NewbieRiskCheck: React.FC = () => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [report, setReport] = useState<NewbieRiskReport | null>(null);
  const toast = useToast();

  const fetchReport = async () => {
    setLoading(true);
    try {
      const data = await getNewbieRiskCheck();
      setReport(data);
    } catch (error) {
      console.error('獲取新手体检报告失败:', error);
      toast({
        title: t('newbieRiskCheck.fetchFailed'),
        description: error instanceof Error ? error.message : t('newbieRiskCheck.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchReport();
  }, []);

  const handleApply = async () => {
    setApplying(true);
    try {
      const response = await applyNewbieSecurityConfig();
      if (response.success) {
        toast({
          title: t('newbieRiskCheck.configUpdated'),
          description: `${response.message} ${t('newbieRiskCheck.applySuccessNotice')}`,
          status: 'success',
          duration: 8000,
          isClosable: true,
        });
        fetchReport(); // 重新獲取报告
      }
    } catch (error) {
      toast({
        title: t('newbieRiskCheck.applyFailed'),
        description: error instanceof Error ? error.message : t('newbieRiskCheck.unknownError'),
        status: 'error',
        duration: 5000,
        isClosable: true,
      });
    } finally {
      setApplying(false);
    }
  };

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'safe': return 'green.500';
      case 'warning': return 'orange.500';
      case 'danger': return 'red.500';
      default: return 'gray.500';
    }
  };

  const getLevelIcon = (level: string) => {
    switch (level) {
      case 'safe': return CheckCircleIcon;
      case 'warning': return WarningIcon;
      case 'danger': return WarningIcon;
      default: return InfoIcon;
    }
  };

  const chartData = report?.results.map(item => ({
    subject: item.item,
    A: item.score,
    fullMark: 100,
  })) || [];

  return (
    <Container maxW="container.xl" py={8}>
      <VStack align="stretch" spacing={6}>
        {/* 页头 */}
        <Box>
          <HStack spacing={3} mb={2}>
            <Text fontSize="3xl">🛡️</Text>
            <Heading size="lg">{t('sidebar.newbieRiskCheck')}</Heading>
          </HStack>
          <Text color="gray.600" fontSize="sm">
            {t('newbieRiskCheck.description')}
          </Text>
        </Box>

        {/* 刷新按钮 */}
        <Box>
          <Button
            size="sm"
            onClick={fetchReport}
            isLoading={loading}
            loadingText={t('newbieRiskCheck.refreshing')}
          >
            {t('common.refresh')}
          </Button>
        </Box>

        {/* 内容区域 */}
        {loading ? (
          <Card>
            <CardBody>
              <VStack py={20}>
                <CircularProgress isIndeterminate color="blue.400" size="80px" />
                <Text mt={4}>{t('newbieRiskCheck.scanning')}</Text>
              </VStack>
            </CardBody>
          </Card>
        ) : report ? (
          <VStack spacing={6} align="stretch">
            {/* 综合评分卡片 */}
            <Card>
              <CardBody>
                <HStack spacing={8} justify="center" p={4}>
                  <Box textAlign="center">
                    <CircularProgress 
                      value={report.overallScore} 
                      color={report.overallScore > 80 ? 'green.400' : report.overallScore > 50 ? 'orange.400' : 'red.400'} 
                      size="120px"
                      thickness="8px"
                    >
                      <CircularProgressLabel fontSize="2xl" fontWeight="bold">
                        {report.overallScore}
                      </CircularProgressLabel>
                    </CircularProgress>
                    <Text mt={2} fontSize="sm" color="gray.600">
                      {t('newbieRiskCheck.overallScore')}
                    </Text>
                  </Box>
                  
                  <Box height="200px" width="300px">
                    <ResponsiveContainer width="100%" height="100%">
                      <RadarChart cx="50%" cy="50%" outerRadius="70%" data={chartData}>
                        <PolarGrid stroke="#E2E8F0" />
                        <PolarAngleAxis dataKey="subject" tick={{ fill: '#718096', fontSize: 12 }} />
                        <Radar
                          name="Score"
                          dataKey="A"
                          stroke="#4299E1"
                          fill="#4299E1"
                          fillOpacity={0.6}
                        />
                      </RadarChart>
                    </ResponsiveContainer>
                  </Box>
                </HStack>
              </CardBody>
            </Card>

            {/* 详细結果 */}
            <Card>
              <CardBody>
                <Heading size="md" mb={4}>{t('newbieRiskCheck.detailedResults')}</Heading>
                <List spacing={4}>
                  {report.results.map((result, index) => (
                    <ListItem key={index} p={4} bg="gray.50" borderRadius="md">
                      <HStack align="start" spacing={3}>
                        <Icon as={getLevelIcon(result.level)} color={getLevelColor(result.level)} mt={1} />
                        <VStack align="start" spacing={1} flex={1}>
                          <HStack justify="space-between" width="100%">
                            <Text fontWeight="bold">{result.item}</Text>
                            <Badge colorScheme={result.level === 'safe' ? 'green' : result.level === 'warning' ? 'orange' : 'red'}>
                              {t('newbieRiskCheck.score')}: {result.score}
                            </Badge>
                          </HStack>
                          <Text fontSize="sm" color="gray.700">{result.message}</Text>
                          <Text fontSize="xs" color="gray.500" fontStyle="italic">
                            💡 {t('newbieRiskCheck.advice')}: {result.advice}
                          </Text>
                        </VStack>
                      </HStack>
                    </ListItem>
                  ))}
                </List>
              </CardBody>
            </Card>

            {/* 重要提示 */}
            <Alert status="warning" borderRadius="md">
              <AlertIcon />
              <Box flex="1">
                <AlertTitle fontSize="sm" mb={1}>
                  {t('newbieRiskCheck.importantNotice')}
                </AlertTitle>
                <AlertDescription fontSize="sm">
                  {t('newbieRiskCheck.leverageNotice')}
                </AlertDescription>
              </Box>
            </Alert>

            {/* 操作按钮 */}
            <Card>
              <CardBody>
                <VStack spacing={4} align="stretch">
                  <HStack justify="flex-end">
                    <Button 
                      colorScheme="blue" 
                      leftIcon={<CheckCircleIcon />} 
                      onClick={handleApply}
                      isLoading={applying}
                      loadingText={t('newbieRiskCheck.applying')}
                    >
                      {t('newbieRiskCheck.applySecurity')}
                    </Button>
                  </HStack>
                </VStack>
              </CardBody>
            </Card>
          </VStack>
        ) : (
          <Card>
            <CardBody>
              <Text textAlign="center" color="gray.500">
                {t('newbieRiskCheck.noReport')}
              </Text>
            </CardBody>
          </Card>
        )}
      </VStack>
    </Container>
  );
};

export default NewbieRiskCheck;
